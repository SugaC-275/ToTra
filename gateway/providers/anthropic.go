package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type AnthropicAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewAnthropicAdapter(baseURL, apiKey string) *AnthropicAdapter {
	return &AnthropicAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (a *AnthropicAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractAnthropicUsage(respBody), nil
}

// ForwardStream sends the request to Anthropic with stream=true and delivers
// each SSE data line to onChunk. Empty lines and [DONE] markers are skipped.
func (a *AnthropicAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	body = injectStreamTrue(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("anthropic: create stream request: %w", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

type anthropicResponse struct {
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func extractAnthropicUsage(body []byte) *Usage {
	var r anthropicResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{PromptTokens: r.Usage.InputTokens, CompletionTokens: r.Usage.OutputTokens}
}

func init() {
	Register("anthropic", func(baseURL, apiKey string) Adapter {
		return NewAnthropicAdapter(baseURL, apiKey)
	})
}

func (a *AnthropicAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model":  model,
		"system": "以下是用户上传的文档内容：\n\n" + docText,
		"messages": []map[string]string{
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body) // cannot fail: map has only string keys and basic value types
	return b
}
