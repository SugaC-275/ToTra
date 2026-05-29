package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	replicateBaseURL    = "https://api.replicate.com/v1"
	replicatePollPeriod = 2 * time.Second
	replicatePollMax    = 5 * time.Minute
)

// ReplicateAdapter calls the Replicate predictions API and polls until completion.
// baseURL format: replicate://{owner}/{model-name} or replicate://{version-hash}
// API keys use the r8_ prefix.
type ReplicateAdapter struct {
	owner   string
	model   string
	version string
	apiKey  string
	client  *http.Client
}

func parseReplicateBaseURL(baseURL string) (owner, model, version string) {
	path := strings.TrimPrefix(baseURL, "replicate://")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], ""
	}
	// bare version hash
	return "", "", path
}

func NewReplicateAdapter(baseURL, apiKey string) *ReplicateAdapter {
	owner, model, version := parseReplicateBaseURL(baseURL)
	return &ReplicateAdapter{
		owner:   owner,
		model:   model,
		version: version,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

type replicatePrediction struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output"`
	Error  string      `json:"error"`
}

func (a *ReplicateAdapter) createPrediction(ctx context.Context, input map[string]interface{}) (*replicatePrediction, error) {
	payload := map[string]interface{}{"input": input}
	if a.version != "" {
		payload["version"] = a.version
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("replicate: marshal input: %w", err)
	}

	url := replicateBaseURL + "/predictions"
	if a.owner != "" && a.model != "" {
		url = fmt.Sprintf("%s/models/%s/%s/predictions", replicateBaseURL, a.owner, a.model)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("replicate: create prediction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replicate: create prediction: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var pred replicatePrediction
	if err := json.Unmarshal(body, &pred); err != nil {
		return nil, fmt.Errorf("replicate: parse prediction response: %w", err)
	}
	return &pred, nil
}

func (a *ReplicateAdapter) pollPrediction(ctx context.Context, id string) (*replicatePrediction, error) {
	deadline := time.Now().Add(replicatePollMax)
	url := fmt.Sprintf("%s/predictions/%s", replicateBaseURL, id)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("replicate: prediction %s timed out after %s", id, replicatePollMax)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("replicate: poll request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+a.apiKey)

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("replicate: poll prediction: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var pred replicatePrediction
		if err := json.Unmarshal(body, &pred); err != nil {
			return nil, fmt.Errorf("replicate: parse poll response: %w", err)
		}

		switch pred.Status {
		case "succeeded":
			return &pred, nil
		case "failed", "canceled":
			return nil, fmt.Errorf("replicate: prediction %s: %s — %s", id, pred.Status, pred.Error)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(replicatePollPeriod):
		}
	}
}

// openAIToReplicateInput converts an OpenAI chat body to a Replicate-style input map.
// Replicate inputs are model-specific; we use prompt + system_prompt as common fields.
func openAIToReplicateInput(body []byte) (map[string]interface{}, error) {
	var req struct {
		Messages  []openAIMessage `json:"messages"`
		MaxTokens int             `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("replicate: parse openai body: %w", err)
	}

	input := map[string]interface{}{}
	var system, prompt strings.Builder
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			system.WriteString(m.Content)
		default:
			if prompt.Len() > 0 {
				prompt.WriteByte('\n')
			}
			fmt.Fprintf(&prompt, "[%s]: %s", m.Role, m.Content)
		}
	}
	if system.Len() > 0 {
		input["system_prompt"] = system.String()
	}
	input["prompt"] = prompt.String()
	if req.MaxTokens > 0 {
		input["max_new_tokens"] = req.MaxTokens
	}
	return input, nil
}

// replicateOutputToOpenAI converts a prediction output to OpenAI chat completion format.
// Replicate outputs vary by model; we join string arrays or use a plain string.
func replicateOutputToOpenAI(pred *replicatePrediction) ([]byte, *Usage, error) {
	var text string
	switch v := pred.Output.(type) {
	case string:
		text = v
	case []interface{}:
		var sb strings.Builder
		for _, tok := range v {
			if s, ok := tok.(string); ok {
				sb.WriteString(s)
			}
		}
		text = sb.String()
	default:
		b, _ := json.Marshal(pred.Output)
		text = string(b)
	}

	resp := map[string]interface{}{
		"id":     "replicate-" + pred.ID,
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return nil, nil, fmt.Errorf("replicate: marshal openai response: %w", err)
	}
	return b, &Usage{}, nil
}

func (a *ReplicateAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	input, err := openAIToReplicateInput(body)
	if err != nil {
		return nil, nil, err
	}

	pred, err := a.createPrediction(ctx, input)
	if err != nil {
		return nil, nil, err
	}

	pred, err = a.pollPrediction(ctx, pred.ID)
	if err != nil {
		return nil, nil, err
	}

	respBody, usage, err := replicateOutputToOpenAI(pred)
	if err != nil {
		return nil, nil, err
	}

	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       respBody,
	}, usage, nil
}

// ForwardStream polls the prediction and delivers a single synthetic SSE chunk on completion.
// Replicate's async model doesn't support true streaming without a webhook or server-sent stream URL.
func (a *ReplicateAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	input, err := openAIToReplicateInput(body)
	if err != nil {
		return err
	}

	pred, err := a.createPrediction(ctx, input)
	if err != nil {
		return err
	}

	pred, err = a.pollPrediction(ctx, pred.ID)
	if err != nil {
		return err
	}

	respBody, _, err := replicateOutputToOpenAI(pred)
	if err != nil {
		return err
	}

	chunk := append([]byte("data: "), respBody...)
	chunk = append(chunk, '\n')
	return onChunk(chunk)
}

func (a *ReplicateAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
	Register("replicate", func(baseURL, apiKey string) Adapter {
		return NewReplicateAdapter(baseURL, apiKey)
	})
}
