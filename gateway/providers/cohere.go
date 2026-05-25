package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const cohereDefaultBaseURL = "https://api.cohere.com"

// CohereAdapter implements the Adapter interface for the Cohere v2 Chat API.
// It translates OpenAI-shaped requests into Cohere native format and converts
// responses back. Cohere-specific extensions can be injected via fields prefixed
// with "_cohere_" in the incoming request body (e.g. "_cohere_documents",
// "_cohere_connectors", "_cohere_search_queries_only").
type CohereAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewCohereAdapter(baseURL, apiKey string) *CohereAdapter {
	return &CohereAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

// translateRequest converts an OpenAI-shaped body to a Cohere v2 chat request.
// Any field starting with "_cohere_" is stripped of its prefix and injected
// directly into the Cohere payload, enabling native-feature pass-through.
func translateRequest(body []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("cohere: parse request body: %w", err)
	}

	out := make(map[string]json.RawMessage)

	// Copy standard OpenAI fields that map 1:1 to Cohere v2.
	for _, field := range []string{"model", "messages", "stream", "temperature", "max_tokens",
		"stop", "top_p", "frequency_penalty", "presence_penalty"} {
		if v, ok := raw[field]; ok {
			out[field] = v
		}
	}

	// Promote _cohere_* extension fields: strip the prefix and inject them.
	for k, v := range raw {
		if strings.HasPrefix(k, "_cohere_") {
			nativeKey := strings.TrimPrefix(k, "_cohere_")
			out[nativeKey] = v
		}
	}

	return json.Marshal(out)
}

// translateResponse converts a Cohere v2 chat response to OpenAI format.
func translateResponse(body []byte) ([]byte, error) {
	var cr cohereResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("cohere: parse response: %w", err)
	}

	content := ""
	if len(cr.Message.Content) > 0 {
		content = cr.Message.Content[0].Text
	}

	finishReason := "stop"
	if cr.FinishReason != "" && cr.FinishReason != "COMPLETE" {
		finishReason = strings.ToLower(cr.FinishReason)
	}

	oai := map[string]interface{}{
		"id":     cr.ID,
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     cr.Usage.BilledUnits.InputTokens,
			"completion_tokens": cr.Usage.BilledUnits.OutputTokens,
			"total_tokens":      cr.Usage.BilledUnits.InputTokens + cr.Usage.BilledUnits.OutputTokens,
		},
	}
	return json.Marshal(oai)
}

func (a *CohereAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	cohereBody, err := translateRequest(body)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v2/chat", bytes.NewReader(cohereBody))
	if err != nil {
		return nil, nil, fmt.Errorf("cohere: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cohere: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("cohere: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, &Usage{}, nil
	}

	usage := extractCohereUsage(respBody)

	oaiBody, err := translateResponse(respBody)
	if err != nil {
		// Return raw body unchanged on translation failure so the caller sees the upstream error.
		return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody}, usage, nil
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: oaiBody}, usage, nil
}

// ForwardStream sends the request to Cohere with stream=true and translates
// Cohere SSE events (content-delta, message-end) into OpenAI SSE format before
// passing each chunk to onChunk.
func (a *CohereAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	cohereBody, err := translateRequest(body)
	if err != nil {
		return err
	}
	cohereBody = injectStreamTrue(cohereBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v2/chat", bytes.NewReader(cohereBody))
	if err != nil {
		return fmt.Errorf("cohere: create stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("cohere: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cohere: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readCohereSSE(resp.Body, onChunk)
}

// readCohereSSE reads Cohere's SSE stream and converts each relevant event into
// an OpenAI-compatible SSE data line delivered to onChunk.
//
// Cohere SSE events of interest:
//
//	event: content-delta   -> partial text in data.delta.message.content.text
//	event: message-end     -> stream termination
func readCohereSSE(r io.Reader, onChunk func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			currentEvent = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}

		switch currentEvent {
		case "content-delta":
			text, err := extractCohereContentDelta([]byte(payload))
			if err != nil || text == "" {
				continue
			}
			oaiChunk := map[string]interface{}{
				"object": "chat.completion.chunk",
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{"content": text},
					},
				},
			}
			b, err := json.Marshal(oaiChunk)
			if err != nil {
				continue
			}
			if err := onChunk([]byte("data: " + string(b) + "\n")); err != nil {
				return err
			}

		case "message-end":
			// Signal stream termination with an OpenAI-style [DONE] marker.
			if err := onChunk([]byte("data: [DONE]\n")); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// extractCohereContentDelta pulls the text out of a content-delta event payload.
// Cohere format: {"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":"..."}}}}
func extractCohereContentDelta(data []byte) (string, error) {
	var ev struct {
		Delta struct {
			Message struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return "", err
	}
	return ev.Delta.Message.Content.Text, nil
}

// cohereResponse mirrors the Cohere v2 chat response shape used for translation.
type cohereResponse struct {
	ID           string `json:"id"`
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Usage struct {
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
	} `json:"usage"`
}

func extractCohereUsage(body []byte) *Usage {
	var r cohereResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{
		PromptTokens:     r.Usage.BilledUnits.InputTokens,
		CompletionTokens: r.Usage.BilledUnits.OutputTokens,
	}
}

func (a *CohereAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "以下是用户上传的文档内容：\n\n" + docText},
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body) // cannot fail: map has only string keys and basic value types
	return b
}

func init() {
	Register("cohere", func(baseURL, apiKey string) Adapter {
		if baseURL == "" {
			baseURL = cohereDefaultBaseURL
		}
		return NewCohereAdapter(baseURL, apiKey)
	})
}
