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
	Register("databricks", func(baseURL, apiKey string) Adapter {
		return NewDatabricksAdapter(baseURL, apiKey)
	})
}

// DatabricksAdapter forwards requests to Databricks Model Serving.
// baseURL is the full serving endpoint invocation URL, e.g.
// "https://{workspace}.azuredatabricks.net/serving-endpoints/{endpoint-name}/invocations"
// The request/response format is OpenAI-compatible.
type DatabricksAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewDatabricksAdapter(baseURL, apiKey string) *DatabricksAdapter {
	return &DatabricksAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

// NewDatabricksAdapterWithClient creates a DatabricksAdapter with an injected HTTP
// client and a custom base URL. Intended for unit tests only.
func NewDatabricksAdapterWithClient(baseURL, apiKey string, client *http.Client) *DatabricksAdapter {
	return &DatabricksAdapter{baseURL: baseURL, apiKey: apiKey, client: client}
}

func (a *DatabricksAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("databricks: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("databricks: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("databricks: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}

// ForwardStream streams a Databricks Model Serving response. The endpoint is
// OpenAI-compatible so we inject stream=true and read SSE chunks as usual.
func (a *DatabricksAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	body = injectStreamTrue(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("databricks: create stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("databricks: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("databricks: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *DatabricksAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
