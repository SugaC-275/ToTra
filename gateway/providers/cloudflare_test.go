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

func TestCloudflareAdapter_Forward_URLAndHeaders(t *testing.T) {
	cfResp := `{"result":{"response":"Hello from Cloudflare"},"success":true}`

	var capturedPath, capturedAuth string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cfResp))
	}))
	defer upstream.Close()

	adapter := providers.NewCloudflareAdapterWithClient("acct123", "cf-api-key", upstream.URL, &http.Client{})
	body := `{"model":"@cf/meta/llama-3-8b-instruct","messages":[{"role":"user","content":"Hello"}]}`

	result, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "Bearer cf-api-key", capturedAuth)
	assert.Equal(t, "/client/v4/accounts/acct123/ai/run/@cf/meta/llama-3-8b-instruct", capturedPath)
}

func TestCloudflareAdapter_Forward_ResponseTranslation(t *testing.T) {
	cfResp := `{"result":{"response":"42 is the answer"},"success":true}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cfResp))
	}))
	defer upstream.Close()

	adapter := providers.NewCloudflareAdapterWithClient("acct123", "key", upstream.URL, &http.Client{})
	body := `{"model":"@cf/meta/llama-3-8b-instruct","messages":[{"role":"user","content":"What is 6*7?"}]}`

	result, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(result.Body, &got))
	assert.Equal(t, "chat.completion", got["object"])
	choices := got["choices"].([]interface{})
	require.Len(t, choices, 1)
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "42 is the answer", msg["content"])
}

func TestCloudflareAdapter_Forward_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success":false,"errors":[{"message":"Unauthorized"}]}`))
	}))
	defer upstream.Close()

	adapter := providers.NewCloudflareAdapterWithClient("acct123", "bad-key", upstream.URL, &http.Client{})
	body := `{"model":"@cf/meta/llama-3-8b-instruct","messages":[{"role":"user","content":"hi"}]}`

	result, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err) // HTTP errors propagate as status, not Go errors
	assert.Equal(t, http.StatusUnauthorized, result.StatusCode)
}

func TestCloudflareAdapter_ForwardStream(t *testing.T) {
	chunks := []string{
		`data: {"response":"Hello"}`,
		`data: {"response":" World"}`,
		`data: [DONE]`,
	}

	var capturedAuth string
	var streamBodyHasStreamTrue bool

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		streamBodyHasStreamTrue = reqBody["stream"] == true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			w.Write([]byte(c + "\n"))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	adapter := providers.NewCloudflareAdapterWithClient("acct123", "stream-key", upstream.URL, &http.Client{})
	body := `{"model":"@cf/meta/llama-3-8b-instruct","messages":[{"role":"user","content":"Hi"}]}`

	var received []string
	err := adapter.ForwardStream(context.Background(), []byte(body), func(chunk []byte) error {
		received = append(received, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer stream-key", capturedAuth)
	assert.True(t, streamBodyHasStreamTrue, "stream:true must be injected into request body")
	require.Len(t, received, 2) // [DONE] skipped
	assert.True(t, strings.HasPrefix(received[0], "data: "))
}

func TestCloudflareAdapter_ForwardStream_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"success":false}`))
	}))
	defer upstream.Close()

	adapter := providers.NewCloudflareAdapterWithClient("acct123", "key", upstream.URL, &http.Client{})
	err := adapter.ForwardStream(context.Background(), []byte(`{"model":"@cf/meta/llama-3-8b-instruct","messages":[]}`), func([]byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestCloudflareAdapter_Forward_MissingModel(t *testing.T) {
	adapter := providers.NewCloudflareAdapterWithClient("acct123", "key", "http://localhost", &http.Client{})
	_, _, err := adapter.Forward(context.Background(), []byte(`{"messages":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

func TestCloudflareAdapter_ParseInvalidBaseURL(t *testing.T) {
	adapter := providers.NewCloudflareAdapter("not-cloudflare://bad", "key")
	_, _, err := adapter.Forward(context.Background(), []byte(`{"model":"@cf/meta/llama-3-8b-instruct","messages":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloudflare:")
}

func TestCloudflareAdapter_BuildFilePrompt(t *testing.T) {
	adapter := providers.NewCloudflareAdapter("cloudflare://acct123", "key")
	out := adapter.BuildFilePrompt("@cf/meta/llama-3-8b-instruct", "doc content", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "@cf/meta/llama-3-8b-instruct", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0].(map[string]interface{})["content"], "doc content")
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
}

func TestCloudflareAdapter_RegisteredInRegistry(t *testing.T) {
	adapter, err := providers.New("cloudflare", "cloudflare://acct123", "key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
