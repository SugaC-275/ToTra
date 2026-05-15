package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewOpenAIAdapter(baseURL, apiKey string) *OpenAIAdapter {
	return &OpenAIAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (a *OpenAIAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("openai: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("openai: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}

type openAIResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func extractOpenAIUsage(body []byte) *Usage {
	var r openAIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{PromptTokens: r.Usage.PromptTokens, CompletionTokens: r.Usage.CompletionTokens}
}

func init() {
	Register("openai", func(baseURL, apiKey string) Adapter {
		return NewOpenAIAdapter(baseURL, apiKey)
	})
}

func (a *OpenAIAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
