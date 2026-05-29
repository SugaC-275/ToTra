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

const hfInferenceBase = "https://api-inference.huggingface.co"

// HuggingFaceAdapter calls the HuggingFace Inference API.
// baseURL modes:
//   - "huggingface://{model_id}" — raw text-generation endpoint
//   - empty or "https://api-inference.huggingface.co/v1" — OpenAI-compat TGI endpoint (falls through to openai factory via knownProviders)
//
// When a model_id is embedded in the baseURL, we POST to /models/{model_id}
// and map HF output back to OpenAI format.
type HuggingFaceAdapter struct {
	modelID string // set when using raw endpoint
	baseURL string // set when using openai-compat TGI endpoint
	apiKey  string
	client  *http.Client
}

func parseHFBaseURL(baseURL string) (modelID, compatURL string) {
	if strings.HasPrefix(baseURL, "huggingface://") {
		return strings.TrimPrefix(baseURL, "huggingface://"), ""
	}
	return "", baseURL
}

func NewHuggingFaceAdapter(baseURL, apiKey string) *HuggingFaceAdapter {
	modelID, compatURL := parseHFBaseURL(baseURL)
	return &HuggingFaceAdapter{
		modelID: modelID,
		baseURL: compatURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// hfTextGenRequest is the payload for the raw HF text-generation task.
type hfTextGenRequest struct {
	Inputs     string         `json:"inputs"`
	Parameters hfTGParameters `json:"parameters,omitempty"`
}

type hfTGParameters struct {
	MaxNewTokens int  `json:"max_new_tokens,omitempty"`
	ReturnFullText bool `json:"return_full_text"`
}

// hfTextGenResponse is the response from the raw HF text-generation endpoint.
type hfTextGenResponse []struct {
	GeneratedText string `json:"generated_text"`
}

// messagesAsPrompt collapses OpenAI messages into a single prompt string.
func messagesAsPrompt(messages []openAIMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "system":
			fmt.Fprintf(&sb, "System: %s\n", m.Content)
		case "user":
			fmt.Fprintf(&sb, "User: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(&sb, "Assistant: %s\n", m.Content)
		}
	}
	sb.WriteString("Assistant:")
	return sb.String()
}

func (a *HuggingFaceAdapter) rawForward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("huggingface: parse openai body: %w", err)
	}

	hfReq := hfTextGenRequest{
		Inputs: messagesAsPrompt(req.Messages),
		Parameters: hfTGParameters{
			MaxNewTokens:   req.MaxTokens,
			ReturnFullText: false,
		},
	}
	hfBody, err := json.Marshal(hfReq)
	if err != nil {
		return nil, nil, fmt.Errorf("huggingface: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s", hfInferenceBase, a.modelID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(hfBody))
	if err != nil {
		return nil, nil, fmt.Errorf("huggingface: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("huggingface: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("huggingface: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, nil, nil
	}

	var hfResp hfTextGenResponse
	if err := json.Unmarshal(respBody, &hfResp); err != nil || len(hfResp) == 0 {
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, &Usage{}, nil
	}

	text := hfResp[0].GeneratedText
	openAIResp := map[string]interface{}{
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
		return nil, nil, fmt.Errorf("huggingface: marshal openai response: %w", err)
	}
	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       out,
	}, &Usage{}, nil
}

func (a *HuggingFaceAdapter) compatForward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	base := a.baseURL
	if base == "" {
		base = hfInferenceBase + "/v1"
	}
	inner := NewOpenAIAdapter(base, a.apiKey)
	return inner.Forward(ctx, body)
}

func (a *HuggingFaceAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	if a.modelID != "" {
		return a.rawForward(ctx, body)
	}
	return a.compatForward(ctx, body)
}

func (a *HuggingFaceAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	if a.modelID != "" {
		// Raw endpoint doesn't support streaming; return a single synthetic chunk.
		result, _, err := a.rawForward(ctx, body)
		if err != nil {
			return err
		}
		chunk := append([]byte("data: "), result.Body...)
		chunk = append(chunk, '\n')
		return onChunk(chunk)
	}

	base := a.baseURL
	if base == "" {
		base = hfInferenceBase + "/v1"
	}
	inner := NewOpenAIAdapter(base, a.apiKey)
	return inner.ForwardStream(ctx, body, onChunk)
}

func (a *HuggingFaceAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
	Register("huggingface", func(baseURL, apiKey string) Adapter {
		return NewHuggingFaceAdapter(baseURL, apiKey)
	})
}
