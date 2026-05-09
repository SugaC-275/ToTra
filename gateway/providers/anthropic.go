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
