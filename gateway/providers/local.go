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
