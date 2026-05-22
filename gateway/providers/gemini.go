package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func init() {
	Register("gemini", func(baseURL, apiKey string) Adapter {
		return NewGeminiAdapter(baseURL, apiKey)
	})
}

type GeminiAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGeminiAdapter(baseURL, apiKey string) *GeminiAdapter {
	return &GeminiAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

// Forward sends the request to Gemini. The model name is extracted from the
// request body and embedded in the URL path, as required by the Gemini API.
// Auth uses ?key= query parameter (no Bearer token).
func (a *GeminiAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		return nil, nil, fmt.Errorf("gemini: model field missing in request body")
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", a.baseURL, req.Model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: read response: %w", err)
	}
	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractGeminiUsage(respBody), nil
}

// ForwardStream sends the request to Gemini via streamGenerateContent and delivers
// each SSE data line to onChunk. Empty lines and [DONE] markers are skipped.
func (a *GeminiAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	var reqFields struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &reqFields); err != nil || reqFields.Model == "" {
		return fmt.Errorf("gemini: model field missing in stream request body")
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s&alt=sse",
		a.baseURL, reqFields.Model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("gemini: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gemini: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gemini: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *GeminiAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"contents": []map[string]interface{}{
			{"role": "user", "parts": []map[string]string{
				{"text": "以下是用户上传的文档内容：\n\n" + docText + "\n\n" + userMessage},
			}},
		},
	}
	b, _ := json.Marshal(body) // cannot fail: map has only string keys and basic value types
	return b
}

type geminiResponse struct {
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

func extractGeminiUsage(body []byte) *Usage {
	var r geminiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{
		PromptTokens:     r.UsageMetadata.PromptTokenCount,
		CompletionTokens: r.UsageMetadata.CandidatesTokenCount,
	}
}
