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

func TestGeminiAdapter_Forward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "gemini-1.5-pro")
		assert.Contains(t, r.URL.RawQuery, "key=test-gemini-key")
		assert.Equal(t, "", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`))
	}))
	defer upstream.Close()

	a := providers.NewGeminiAdapter(upstream.URL, "test-gemini-key")
	body := `{"model":"gemini-1.5-pro","contents":[{"role":"user","parts":[{"text":"Hi"}]}]}`
	resp, usage, err := a.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
}

func TestGeminiAdapter_BuildFilePrompt(t *testing.T) {
	a := providers.NewGeminiAdapter("http://x", "key")
	body := a.BuildFilePrompt("gemini-1.5-pro", "doc content", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "gemini-1.5-pro", got["model"])
	contents := got["contents"].([]interface{})
	require.Len(t, contents, 1)
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	text := parts[0].(map[string]interface{})["text"].(string)
	assert.Contains(t, text, "doc content")
	assert.Contains(t, text, "summarize")
}

func TestGeminiAdapter_RegistryLookup(t *testing.T) {
	adapter, err := providers.New("gemini", "http://x", "key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
