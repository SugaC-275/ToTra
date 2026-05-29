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

const ai21BaseURL = "https://api.ai21.com/studio/v1"

// AI21Adapter calls AI21 Labs APIs.
// Jamba models use the OpenAI-compat /chat/completions endpoint.
// Jurassic models use the older /complete endpoint with a mapped prompt format.
type AI21Adapter struct {
	apiKey string
	client *http.Client
}

func NewAI21Adapter(_, apiKey string) *AI21Adapter {
	return &AI21Adapter{apiKey: apiKey, client: &http.Client{}}
}

// isJamba returns true for Jamba-series models which use OpenAI-compat API.
func isJamba(model string) bool {
	return strings.HasPrefix(model, "jamba")
}

// ai21ModelFromBody extracts the "model" field from the request body.
func ai21ModelFromBody(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &req) //nolint:errcheck — empty string on failure is handled downstream
	return req.Model
}

// jambaForward delegates to the OpenAI-compat endpoint (Jamba models).
func (a *AI21Adapter) jambaForward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	inner := NewOpenAIAdapter(ai21BaseURL, a.apiKey)
	return inner.Forward(ctx, body)
}

func (a *AI21Adapter) jambaStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	inner := NewOpenAIAdapter(ai21BaseURL, a.apiKey)
	return inner.ForwardStream(ctx, body, onChunk)
}

// jurassicRequest is the AI21 Jurassic complete API body.
type jurassicRequest struct {
	Prompt    string `json:"prompt"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

// jurassicResponse is a partial decode of the Jurassic /complete response.
type jurassicResponse struct {
	ID          string `json:"id"`
	Completions []struct {
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	} `json:"completions"`
}

// jurassicForward calls the Jurassic /complete endpoint and maps back to OpenAI format.
// Note: AI21 Jurassic auth uses the key directly without a "Bearer " prefix.
func (a *AI21Adapter) jurassicForward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("ai21: parse openai body: %w", err)
	}

	model := ai21ModelFromBody(body)
	j2Req := jurassicRequest{
		Prompt:    messagesAsPrompt(req.Messages),
		MaxTokens: req.MaxTokens,
	}
	j2Body, err := json.Marshal(j2Req)
	if err != nil {
		return nil, nil, fmt.Errorf("ai21: marshal jurassic request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/complete", ai21BaseURL, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(j2Body))
	if err != nil {
		return nil, nil, fmt.Errorf("ai21: create request: %w", err)
	}
	// Jurassic API uses key without Bearer prefix.
	httpReq.Header.Set("Authorization", a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("ai21: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("ai21: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, nil, nil
	}

	var j2Resp jurassicResponse
	if err := json.Unmarshal(respBody, &j2Resp); err != nil || len(j2Resp.Completions) == 0 {
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, &Usage{}, nil
	}

	text := j2Resp.Completions[0].Data.Text
	openAIResp := map[string]interface{}{
		"id":     j2Resp.ID,
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}
	out, err := json.Marshal(openAIResp)
	if err != nil {
		return nil, nil, fmt.Errorf("ai21: marshal openai response: %w", err)
	}
	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       out,
	}, &Usage{}, nil
}

func (a *AI21Adapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	if isJamba(ai21ModelFromBody(body)) {
		return a.jambaForward(ctx, body)
	}
	return a.jurassicForward(ctx, body)
}

func (a *AI21Adapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	if isJamba(ai21ModelFromBody(body)) {
		return a.jambaStream(ctx, body, onChunk)
	}
	// Jurassic doesn't support streaming; return a single synthetic chunk.
	result, _, err := a.jurassicForward(ctx, body)
	if err != nil {
		return err
	}
	chunk := append([]byte("data: "), result.Body...)
	chunk = append(chunk, '\n')
	return onChunk(chunk)
}

func (a *AI21Adapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
	Register("ai21", func(baseURL, apiKey string) Adapter {
		return NewAI21Adapter(baseURL, apiKey)
	})
}
