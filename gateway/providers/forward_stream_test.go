package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

// sseBody is a minimal SSE response with two data events and a [DONE] terminator.
const sseBody = "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\ndata: [DONE]\n"

func collectChunks(t *testing.T, streamFn func(func([]byte) error) error) []string {
	t.Helper()
	var chunks []string
	err := streamFn(func(chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	})
	require.NoError(t, err)
	return chunks
}

// --- OpenAI ---

func TestOpenAIAdapter_ForwardStream_SendsStreamTrue(t *testing.T) {
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.Body
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer upstream.Close()
	_ = gotBody

	a := providers.NewOpenAIAdapter(upstream.URL, "key")
	body := []byte(`{"model":"gpt-4o","stream":false,"messages":[]}`)
	chunks := collectChunks(t, func(cb func([]byte) error) error {
		return a.ForwardStream(context.Background(), body, cb)
	})
	assert.Len(t, chunks, 1) // [DONE] is skipped
	assert.Contains(t, chunks[0], `"delta"`)
}

func TestOpenAIAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"oops"}`))
	}))
	defer upstream.Close()

	a := providers.NewOpenAIAdapter(upstream.URL, "key")
	err := a.ForwardStream(context.Background(), []byte(`{"model":"gpt-4o"}`), func(_ []byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestOpenAIAdapter_ForwardStream_AuthHeader(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer upstream.Close()

	a := providers.NewOpenAIAdapter(upstream.URL, "test-secret")
	_ = a.ForwardStream(context.Background(), []byte(`{"model":"gpt-4o"}`), func(_ []byte) error { return nil })
	assert.Equal(t, "Bearer test-secret", gotAuth)
}

// --- Anthropic ---

func TestAnthropicAdapter_ForwardStream_DeliverChunks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-claude-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\ndata: [DONE]\n"))
	}))
	defer upstream.Close()

	a := providers.NewAnthropicAdapter(upstream.URL, "test-claude-key")
	chunks := collectChunks(t, func(cb func([]byte) error) error {
		return a.ForwardStream(context.Background(), []byte(`{"model":"claude-3-5-sonnet-20241022","max_tokens":10}`), cb)
	})
	require.Len(t, chunks, 1)
	assert.Contains(t, chunks[0], `"text":"Hello"`)
}

func TestAnthropicAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer upstream.Close()

	a := providers.NewAnthropicAdapter(upstream.URL, "bad-key")
	err := a.ForwardStream(context.Background(), []byte(`{"model":"claude-3-5-sonnet-20241022"}`), func(_ []byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// --- Gemini ---

func TestGeminiAdapter_ForwardStream_UsesStreamEndpoint(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hi\"}]}}]}\n"))
	}))
	defer upstream.Close()

	a := providers.NewGeminiAdapter(upstream.URL, "key")
	body := []byte(`{"model":"gemini-1.5-pro","contents":[]}`)
	_ = a.ForwardStream(context.Background(), body, func(_ []byte) error { return nil })
	assert.Contains(t, gotPath, "streamGenerateContent")
	assert.Contains(t, gotPath, "gemini-1.5-pro")
}

func TestGeminiAdapter_ForwardStream_MissingModelReturnsError(t *testing.T) {
	a := providers.NewGeminiAdapter("http://127.0.0.1:1", "key")
	err := a.ForwardStream(context.Background(), []byte(`{}`), func(_ []byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model field missing")
}

func TestGeminiAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer upstream.Close()

	a := providers.NewGeminiAdapter(upstream.URL, "key")
	err := a.ForwardStream(context.Background(), []byte(`{"model":"gemini-1.5-pro"}`), func(_ []byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// --- Local ---

func TestLocalAdapter_ForwardStream_DeliverChunks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\ndata: [DONE]\n"))
	}))
	defer upstream.Close()

	a := providers.NewLocalAdapter(upstream.URL)
	chunks := collectChunks(t, func(cb func([]byte) error) error {
		return a.ForwardStream(context.Background(), []byte(`{"model":"llama3"}`), cb)
	})
	require.Len(t, chunks, 1)
	assert.Contains(t, chunks[0], `"content":"ok"`)
}
