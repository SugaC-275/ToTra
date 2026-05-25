package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AzureAdapter forwards requests to Azure OpenAI.
// baseURL format: "azure://{resource-name}/{deployment-name}"
// e.g. "azure://mycompany/gpt-4o-prod"
type AzureAdapter struct {
	resource    string
	deployment  string
	apiKey      string
	client      *http.Client
	// testBaseURL overrides endpoint construction when set (used in tests only).
	testBaseURL string
}

const azureAPIVersion = "2024-12-01-preview"

// parseAzureBaseURL extracts resource and deployment from "azure://resource/deployment".
func parseAzureBaseURL(baseURL string) (resource, deployment string, err error) {
	path := strings.TrimPrefix(baseURL, "azure://")
	if path == baseURL {
		return "", "", fmt.Errorf("azure: baseURL must start with azure://, got %q", baseURL)
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("azure: baseURL must be azure://{resource}/{deployment}, got %q", baseURL)
	}
	return parts[0], parts[1], nil
}

func NewAzureAdapter(baseURL, apiKey string) *AzureAdapter {
	resource, deployment, err := parseAzureBaseURL(baseURL)
	if err != nil {
		// Return adapter with empty fields; Forward/ForwardStream will surface the error.
		return &AzureAdapter{apiKey: apiKey, client: &http.Client{}}
	}
	return &AzureAdapter{
		resource:   resource,
		deployment: deployment,
		apiKey:     apiKey,
		client:     &http.Client{},
	}
}

// NewAzureAdapterWithClient creates an AzureAdapter that sends all requests to
// rawBaseURL (without azure:// parsing). Intended for unit tests using httptest.Server.
func NewAzureAdapterWithClient(rawBaseURL, apiKey string, client *http.Client) *AzureAdapter {
	return &AzureAdapter{
		apiKey:      apiKey,
		client:      client,
		testBaseURL: rawBaseURL,
	}
}

// endpoint returns the full Azure OpenAI chat completions URL.
func (a *AzureAdapter) endpoint() (string, error) {
	if a.testBaseURL != "" {
		return a.testBaseURL + "/chat/completions", nil
	}
	if a.resource == "" || a.deployment == "" {
		return "", fmt.Errorf("azure: invalid baseURL — resource and deployment must be non-empty")
	}
	return fmt.Sprintf(
		"https://%s.openai.azure.com/openai/deployments/%s/chat/completions?api-version=%s",
		a.resource, a.deployment, azureAPIVersion,
	), nil
}

func (a *AzureAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	url, err := a.endpoint()
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("azure: create request: %w", err)
	}
	req.Header.Set("api-key", a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("azure: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("azure: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}

// ForwardStream sends the request with stream=true and delivers each SSE data line to onChunk.
func (a *AzureAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	url, err := a.endpoint()
	if err != nil {
		return err
	}

	body = injectStreamTrue(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("azure: create stream request: %w", err)
	}
	req.Header.Set("api-key", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("azure: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("azure: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *AzureAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "以下是用户上传的文档内容：\n\n" + docText},
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func init() {
	Register("azure", func(baseURL, apiKey string) Adapter {
		return NewAzureAdapter(baseURL, apiKey)
	})
}
