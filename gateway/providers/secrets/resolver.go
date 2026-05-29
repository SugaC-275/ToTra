package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

const cacheTTL = 15 * time.Minute

type entry struct {
	value   string
	expires time.Time
}

// Resolver resolves secret URI references to their plaintext values.
// Supported schemes:
//   - asm://secret-name        → AWS Secrets Manager
//   - akv://vault-name/secret  → Azure Key Vault
//   - gsm://project/secret     → GCP Secret Manager (latest version)
type Resolver struct {
	mu    sync.Mutex
	cache map[string]entry
}

func New() *Resolver {
	return &Resolver{cache: make(map[string]entry)}
}

// Resolve returns the secret value for a URI, or the input unchanged if it is not a URI.
func (r *Resolver) Resolve(ctx context.Context, value string) string {
	if !strings.Contains(value, "://") {
		return value
	}

	r.mu.Lock()
	if e, ok := r.cache[value]; ok && time.Now().Before(e.expires) {
		r.mu.Unlock()
		return e.value
	}
	r.mu.Unlock()

	var resolved string
	var err error

	switch {
	case strings.HasPrefix(value, "asm://"):
		resolved, err = r.resolveASM(ctx, strings.TrimPrefix(value, "asm://"))
	case strings.HasPrefix(value, "akv://"):
		resolved, err = r.resolveAKV(ctx, strings.TrimPrefix(value, "akv://"))
	case strings.HasPrefix(value, "gsm://"):
		resolved, err = r.resolveGSM(ctx, strings.TrimPrefix(value, "gsm://"))
	default:
		return value
	}

	if err != nil {
		slog.Error("secrets resolver", "uri", value, "err", err)
		return value
	}

	r.mu.Lock()
	r.cache[value] = entry{value: resolved, expires: time.Now().Add(cacheTTL)}
	r.mu.Unlock()
	return resolved
}

func (r *Resolver) resolveASM(ctx context.Context, secretName string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("asm: load config: %w", err)
	}
	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretName,
	})
	if err != nil {
		return "", fmt.Errorf("asm: get secret %q: %w", secretName, err)
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	return "", fmt.Errorf("asm: secret %q has no string value", secretName)
}

func (r *Resolver) resolveAKV(ctx context.Context, path string) (string, error) {
	// path format: vault-name/secret-name
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("akv: invalid path %q (expected vault/secret)", path)
	}
	vaultName, secretName := parts[0], parts[1]

	token, err := azureToken(ctx)
	if err != nil {
		return "", fmt.Errorf("akv: get token: %w", err)
	}

	url := fmt.Sprintf("https://%s.vault.azure.net/secrets/%s?api-version=7.4", vaultName, secretName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("akv: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Value string `json:"value"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("akv: decode: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("akv: %s", result.Error.Message)
	}
	return result.Value, nil
}

func (r *Resolver) resolveGSM(ctx context.Context, path string) (string, error) {
	// path format: projects/{project}/secrets/{secret}
	if !strings.HasPrefix(path, "projects/") {
		path = "projects/" + path
	}
	url := fmt.Sprintf("https://secretmanager.googleapis.com/v1/%s/versions/latest:access", path)

	token, err := gsmToken(ctx)
	if err != nil {
		return "", fmt.Errorf("gsm: get token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gsm: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Payload struct {
			Data string `json:"data"` // base64-encoded
		} `json:"payload"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("gsm: decode: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("gsm: %s", result.Error.Message)
	}
	return result.Payload.Data, nil
}

func azureToken(ctx context.Context) (string, error) {
	resp, err := http.DefaultClient.Do(mustRequest(ctx, "GET",
		"http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https://vault.azure.net",
		"Metadata", "true"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("azure IMDS token unavailable")
	}
	return result.AccessToken, nil
}

func gsmToken(ctx context.Context) (string, error) {
	resp, err := http.DefaultClient.Do(mustRequest(ctx, "GET",
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		"Metadata-Flavor", "Google"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("GCP metadata token unavailable")
	}
	return result.AccessToken, nil
}

func mustRequest(ctx context.Context, method, url, headerKey, headerVal string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, url, nil)
	req.Header.Set(headerKey, headerVal)
	return req
}
