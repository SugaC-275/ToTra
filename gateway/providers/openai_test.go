package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

func TestOpenAIAdapter_ForwardRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-openai-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()

	adapter := providers.NewOpenAIAdapter(upstream.URL, "test-openai-key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	resp, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
}

func TestOpenAIAdapter_BuildFilePrompt(t *testing.T) {
	a := providers.NewOpenAIAdapter("http://x", "key")
	body := a.BuildFilePrompt("gpt-4o", "doc content here", "summarize it")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "gpt-4o", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]interface{})["role"])
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
	systemContent := msgs[0].(map[string]interface{})["content"].(string)
	assert.Contains(t, systemContent, "doc content here")
	userContent := msgs[1].(map[string]interface{})["content"].(string)
	assert.Equal(t, "summarize it", userContent)
}
