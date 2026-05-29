package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

func TestDatabricksAdapter_Forward_RequestShape(t *testing.T) {
	responseBody := `{"id":"db-1","choices":[{"message":{"role":"assistant","content":"Hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`

	var capturedAuth, capturedContentType, capturedPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	adapter := providers.NewDatabricksAdapterWithClient(
		upstream.URL+"/serving-endpoints/dbrx/invocations",
		"db-api-key",
		&http.Client{},
	)
	body := `{"model":"databricks-dbrx-instruct","messages":[{"role":"user","content":"Hello"}]}`

	result, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "Bearer db-api-key", capturedAuth)
	assert.Equal(t, "application/json", capturedContentType)
	assert.Equal(t, "/serving-endpoints/dbrx/invocations", capturedPath)
	assert.Equal(t, 5, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
}

func TestDatabricksAdapter_Forward_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer upstream.Close()

	adapter := providers.NewDatabricksAdapterWithClient(upstream.URL, "key", &http.Client{})
	result, _, err := adapter.Forward(context.Background(), []byte(`{"model":"m","messages":[]}`))
	require.NoError(t, err) // HTTP errors propagate as status codes, not Go errors
	assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
}

func TestDatabricksAdapter_ForwardStream(t *testing.T) {
	chunks := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"index":0}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":" there"},"index":0}]}`,
		`data: [DONE]`,
	}

	var capturedAuth string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			w.Write([]byte(c + "\n"))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := providers.NewDatabricksAdapterWithClient(upstream.URL, "stream-key", &http.Client{})
	body := `{"model":"dbrx","messages":[{"role":"user","content":"Hi"}]}`

	var received []string
	err := adapter.ForwardStream(context.Background(), []byte(body), func(chunk []byte) error {
		received = append(received, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer stream-key", capturedAuth)
	require.Len(t, received, 2) // [DONE] skipped
	assert.True(t, strings.HasPrefix(received[0], "data: "))
	assert.Contains(t, received[0], "Hello")
}

func TestDatabricksAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer upstream.Close()

	adapter := providers.NewDatabricksAdapterWithClient(upstream.URL, "key", &http.Client{})
	err := adapter.ForwardStream(context.Background(), []byte(`{"model":"m","messages":[]}`), func([]byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestDatabricksAdapter_BuildFilePrompt(t *testing.T) {
	adapter := providers.NewDatabricksAdapter("https://example.azuredatabricks.net/serving-endpoints/ep/invocations", "key")
	out := adapter.BuildFilePrompt("dbrx", "document content", "summarize this")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "dbrx", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]interface{})["role"])
	assert.Contains(t, msgs[0].(map[string]interface{})["content"], "document content")
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
	assert.Equal(t, "summarize this", msgs[1].(map[string]interface{})["content"])
}

func TestDatabricksAdapter_RegisteredInRegistry(t *testing.T) {
	adapter, err := providers.New("databricks", "https://example.azuredatabricks.net/serving-endpoints/ep/invocations", "key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
