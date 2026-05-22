package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type LocalAdapter struct {
	baseURL string
	client  *http.Client
}

func NewLocalAdapter(baseURL string) *LocalAdapter {
	return &LocalAdapter{baseURL: baseURL, client: &http.Client{}}
}

func (a *LocalAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("local: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("local: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("local: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}

// ForwardStream sends the request to the local model with stream=true and
// delivers each SSE data line to onChunk.
func (a *LocalAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	body = injectStreamTrue(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("local: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("local: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("local: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func init() {
	Register("local", func(baseURL, _ string) Adapter {
		return NewLocalAdapter(baseURL)
	})
}

// BuildFilePrompt returns nil — local models run on-prem so file upload
// scanning is unnecessary. The FileChatHandler checks for nil and returns 400.
func (a *LocalAdapter) BuildFilePrompt(_, _, _ string) []byte { return nil }
