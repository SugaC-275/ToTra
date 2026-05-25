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

// newAzureAdapterWithURL returns an AzureAdapter pointed at the given httptest server URL.
// It bypasses the azure:// scheme parsing by constructing a test-only struct via the
// exported constructor with a dummy baseURL, then overriding endpoint resolution by
// using a thin wrapper around httptest.Server. Because NewAzureAdapter constructs a live
// HTTPS URL from the parsed resource/deployment, we instead expose the endpoint
// indirectly: we mount the mock on a path the adapter will call for
// "azure://testresource/testdeployment" and swap the base transport. The simpler
// approach used here is: add a fake registry entry that returns an adapter whose
// baseURL is replaced with the test server URL.
func azureAdapterAtURL(serverURL, apiKey string) *providers.AzureAdapter {
	// We expose NewAzureAdapter accepting a raw URL when the scheme is NOT azure://,
	// but the current implementation rejects non-azure:// URLs. Instead, create a
	// real adapter with a well-formed azure:// URL and then exercise it through the
	// httptest server by using a custom HTTP client injected via the exported field.
	// Since AzureAdapter.client is unexported, use the providers.NewAzureAdapterWithClient
	// helper introduced for testing.
	return providers.NewAzureAdapterWithClient(serverURL, apiKey, &http.Client{})
}

func TestAzureAdapter_Forward_Success(t *testing.T) {
	responseBody := `{"id":"chatcmpl-az-1","choices":[{"message":{"role":"assistant","content":"Hello from Azure"}}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "test-azure-key", r.Header.Get("api-key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	adapter := azureAdapterAtURL(upstream.URL, "test-azure-key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	result, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 4, usage.CompletionTokens)
}

func TestAzureAdapter_Forward_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"Internal Server Error"}}`))
	}))
	defer upstream.Close()

	adapter := azureAdapterAtURL(upstream.URL, "key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	result, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err) // Forward propagates HTTP status, not an error
	assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
}

func TestAzureAdapter_ForwardStream(t *testing.T) {
	chunks := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"index":0}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":" World"},"index":0}]}`,
		`data: [DONE]`,
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-azure-key", r.Header.Get("api-key"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			w.Write([]byte(c + "\n"))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := azureAdapterAtURL(upstream.URL, "test-azure-key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	var received []string
	err := adapter.ForwardStream(context.Background(), []byte(body), func(chunk []byte) error {
		received = append(received, string(chunk))
		return nil
	})
	require.NoError(t, err)
	// [DONE] is skipped; expect 2 data chunks
	require.Len(t, received, 2)
	assert.True(t, strings.HasPrefix(received[0], "data: "))
	assert.Contains(t, received[0], "Hello")
}

func TestAzureAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer upstream.Close()

	adapter := azureAdapterAtURL(upstream.URL, "key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	err := adapter.ForwardStream(context.Background(), []byte(body), func([]byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestAzureAdapter_BuildFilePrompt(t *testing.T) {
	adapter := azureAdapterAtURL("http://x", "key")
	out := adapter.BuildFilePrompt("gpt-4o", "doc text", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "gpt-4o", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]interface{})["role"])
	assert.Contains(t, msgs[0].(map[string]interface{})["content"], "doc text")
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
	assert.Equal(t, "summarize", msgs[1].(map[string]interface{})["content"])
}

func TestAzureAdapter_ParseInvalidBaseURL(t *testing.T) {
	// An adapter built with an invalid URL should surface an error on Forward.
	adapter := providers.NewAzureAdapter("not-azure://bad", "key")
	_, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure:")
}
