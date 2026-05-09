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

func TestAnthropicAdapter_ForwardRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-claude-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"msg_1","type":"message","content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":8,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	adapter := providers.NewAnthropicAdapter(upstream.URL, "test-claude-key")
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":100,"messages":[{"role":"user","content":"Hi"}]}`

	resp, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
}
