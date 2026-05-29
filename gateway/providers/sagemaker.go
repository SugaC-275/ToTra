package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
)

// SageMakerAdapter forwards requests to an AWS SageMaker endpoint via SigV4-signed HTTP.
// baseURL format: sagemaker://{region}/{endpoint-name}
// The request body is forwarded as-is (OpenAI-compatible by default — TGI/vLLM deployments).
type SageMakerAdapter struct {
	region       string
	endpointName string
	client       *http.Client
}

func parseSageMakerBaseURL(baseURL string) (region, endpointName string, err error) {
	path := strings.TrimPrefix(baseURL, "sagemaker://")
	if path == baseURL {
		return "", "", fmt.Errorf("sagemaker: baseURL must start with sagemaker://, got %q", baseURL)
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("sagemaker: baseURL must be sagemaker://{region}/{endpoint-name}, got %q", baseURL)
	}
	return parts[0], parts[1], nil
}

func NewSageMakerAdapter(baseURL, _ string) *SageMakerAdapter {
	region, endpointName, err := parseSageMakerBaseURL(baseURL)
	if err != nil {
		return &SageMakerAdapter{client: &http.Client{}}
	}
	return &SageMakerAdapter{
		region:       region,
		endpointName: endpointName,
		client:       &http.Client{},
	}
}

func (a *SageMakerAdapter) invokeURL() string {
	return fmt.Sprintf("https://runtime.sagemaker.%s.amazonaws.com/endpoints/%s/invocations",
		a.region, a.endpointName)
}

func bodyHash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func (a *SageMakerAdapter) signAndDo(ctx context.Context, body []byte) (*http.Response, error) {
	if a.region == "" || a.endpointName == "" {
		return nil, fmt.Errorf("sagemaker: invalid baseURL — region and endpoint-name must be non-empty")
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(a.region))
	if err != nil {
		return nil, fmt.Errorf("sagemaker: load AWS config: %w", err)
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("sagemaker: retrieve AWS credentials: %w", err)
	}

	url := a.invokeURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("sagemaker: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	signer := v4.NewSigner()
	if err := signer.SignHTTP(ctx, creds, req, bodyHash(body), "sagemaker", a.region, time.Now()); err != nil {
		return nil, fmt.Errorf("sagemaker: sign request: %w", err)
	}

	return a.client.Do(req)
}

func (a *SageMakerAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	resp, err := a.signAndDo(ctx, body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("sagemaker: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}

func (a *SageMakerAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	body = injectStreamTrue(body)

	resp, err := a.signAndDo(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sagemaker: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *SageMakerAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
	Register("sagemaker", func(baseURL, apiKey string) Adapter {
		return NewSageMakerAdapter(baseURL, apiKey)
	})
}
