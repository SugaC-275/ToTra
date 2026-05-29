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

func init() {
	Register("cloudflare", func(baseURL, apiKey string) Adapter {
		return NewCloudflareAdapter(baseURL, apiKey)
	})
}

// CloudflareAdapter forwards requests to Cloudflare Workers AI.
// baseURL format: "cloudflare://{account_id}"
// e.g. "cloudflare://abc123def456"
type CloudflareAdapter struct {
	accountID string
	apiKey    string
	client    *http.Client

	// testBaseURL overrides the endpoint root for unit tests.
	testBaseURL string
}

func parseCloudflareBaseURL(baseURL string) (accountID string, err error) {
	path := strings.TrimPrefix(baseURL, "cloudflare://")
	if path == baseURL {
		return "", fmt.Errorf("cloudflare: baseURL must start with cloudflare://, got %q", baseURL)
	}
	accountID = strings.TrimRight(path, "/")
	if accountID == "" {
		return "", fmt.Errorf("cloudflare: baseURL must be cloudflare://{account_id}, got %q", baseURL)
	}
	return accountID, nil
}

func NewCloudflareAdapter(baseURL, apiKey string) *CloudflareAdapter {
	accountID, err := parseCloudflareBaseURL(baseURL)
	if err != nil {
		return &CloudflareAdapter{apiKey: apiKey, client: &http.Client{}}
	}
	return &CloudflareAdapter{accountID: accountID, apiKey: apiKey, client: &http.Client{}}
}

// NewCloudflareAdapterWithClient creates a CloudflareAdapter that sends requests to
// testBaseURL instead of api.cloudflare.com. Intended for unit tests only.
func NewCloudflareAdapterWithClient(accountID, apiKey, testBaseURL string, client *http.Client) *CloudflareAdapter {
	return &CloudflareAdapter{
		accountID:   accountID,
		apiKey:      apiKey,
		client:      client,
		testBaseURL: testBaseURL,
	}
}

// endpoint returns the full Cloudflare Workers AI URL for the given model.
func (a *CloudflareAdapter) endpoint(model string) (string, error) {
	if a.accountID == "" {
		return "", fmt.Errorf("cloudflare: invalid baseURL — account_id must be non-empty")
	}
	if model == "" {
		return "", fmt.Errorf("cloudflare: model field missing in request body")
	}
	if a.testBaseURL != "" {
		return fmt.Sprintf("%s/client/v4/accounts/%s/ai/run/%s", a.testBaseURL, a.accountID, model), nil
	}
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s",
		a.accountID, model), nil
}

// translateCloudflareResponse converts Cloudflare Workers AI response to OpenAI format.
func translateCloudflareResponse(body []byte) []byte {
	var resp struct {
		Result  json.RawMessage `json:"result"`
		Success bool            `json:"success"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || !resp.Success {
		return body
	}

	// The result may be a streaming chunk or a full response object.
	// For chat models the result has a "response" field.
	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil || result.Response == "" {
		return body
	}

	out := map[string]interface{}{
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": result.Response,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
		},
	}
	b, _ := json.Marshal(out)
	return b
}

// extractModel reads the "model" field from the JSON body without unmarshaling
// the whole structure. Reuses the package-level helper from vertex.go.
func (a *CloudflareAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	model, err := extractModel(body)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudflare: %w", err)
	}

	epURL, err := a.endpoint(model)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, epURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("cloudflare: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudflare: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudflare: read response: %w", err)
	}

	translated := translateCloudflareResponse(respBody)
	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: translated},
		extractOpenAIUsage(translated), nil
}

func (a *CloudflareAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	model, err := extractModel(body)
	if err != nil {
		return fmt.Errorf("cloudflare: %w", err)
	}

	epURL, err := a.endpoint(model)
	if err != nil {
		return err
	}

	// Inject stream: true into the request body.
	body = injectStreamTrue(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, epURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cloudflare: create stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *CloudflareAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
