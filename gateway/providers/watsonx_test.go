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

// watsonxTestServer sets up a mock server that handles both IAM token exchange
// and WatsonX ML inference on a single httptest.Server. The IAM path is
// /identity/token and inference is /ml/v1/text/generation.
func watsonxTestServer(t *testing.T, iamResp, mlResp string, mlStatus int) (iamURL, mlBaseURL string, close func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(iamResp))
	})
	mux.HandleFunc("/ml/v1/text/generation", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-iam-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(mlStatus)
		w.Write([]byte(mlResp))
	})
	srv := httptest.NewServer(mux)
	return srv.URL + "/identity/token", srv.URL, srv.Close
}

func TestWatsonxAdapter_Forward_RequestAndResponse(t *testing.T) {
	iamResp := `{"access_token":"test-iam-token","expires_in":3600}`
	mlResp := `{"results":[{"generated_text":"Hello from WatsonX"}]}`

	iamURL, mlURL, close := watsonxTestServer(t, iamResp, mlResp, http.StatusOK)
	defer close()

	adapter := providers.NewWatsonxAdapterForTest("us-south", "my-api-key", iamURL, mlURL, &http.Client{})
	body := `{"model":"ibm/granite-13b-chat-v2","messages":[{"role":"user","content":"Hello"}]}`

	result, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)

	// Verify the response was translated to OpenAI format.
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(result.Body, &got))
	choices := got["choices"].([]interface{})
	require.Len(t, choices, 1)
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "Hello from WatsonX", msg["content"])
}

func TestWatsonxAdapter_Forward_TranslatesRequestBody(t *testing.T) {
	iamResp := `{"access_token":"test-iam-token","expires_in":3600}`

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(iamResp))
	})
	mux.HandleFunc("/ml/v1/text/generation", func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		var m json.RawMessage
		dec.Decode(&m)
		capturedBody = m
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results":[{"generated_text":"ok"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	adapter := providers.NewWatsonxAdapterForTest("us-south", "key", srv.URL+"/identity/token", srv.URL, &http.Client{})
	body := `{"model":"ibm/granite-13b-chat-v2","messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"What is 2+2?"}]}`

	_, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)

	var req map[string]interface{}
	require.NoError(t, json.Unmarshal(capturedBody, &req))
	assert.Equal(t, "ibm/granite-13b-chat-v2", req["model_id"])
	assert.Equal(t, "What is 2+2?", req["input"]) // last user message
	params := req["parameters"].(map[string]interface{})
	assert.EqualValues(t, 500, params["max_new_tokens"])
}

func TestWatsonxAdapter_Forward_IAMTokenCached(t *testing.T) {
	iamCallCount := 0
	iamResp := `{"access_token":"cached-token","expires_in":3600}`

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/token", func(w http.ResponseWriter, r *http.Request) {
		iamCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(iamResp))
	})
	mux.HandleFunc("/ml/v1/text/generation", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results":[{"generated_text":"ok"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	adapter := providers.NewWatsonxAdapterForTest("us-south", "key", srv.URL+"/identity/token", srv.URL, &http.Client{})
	body := `{"model":"ibm/granite-13b-chat-v2","messages":[{"role":"user","content":"hi"}]}`

	_, _, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	_, _, err = adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)

	assert.Equal(t, 1, iamCallCount, "IAM token should be cached after first call")
}

func TestWatsonxAdapter_ForwardStream(t *testing.T) {
	iamResp := `{"access_token":"test-iam-token","expires_in":3600}`
	chunks := []string{
		`data: {"results":[{"generated_text":"Hello"}]}`,
		`data: {"results":[{"generated_text":" World"}]}`,
		`data: [DONE]`,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(iamResp))
	})
	mux.HandleFunc("/ml/v1/text/generation", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "true", r.URL.Query().Get("stream"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			w.Write([]byte(c + "\n"))
			flusher.Flush()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	adapter := providers.NewWatsonxAdapterForTest("us-south", "key", srv.URL+"/identity/token", srv.URL, &http.Client{})
	body := `{"model":"ibm/granite-13b-chat-v2","messages":[{"role":"user","content":"Hi"}]}`

	var received []string
	err := adapter.ForwardStream(context.Background(), []byte(body), func(chunk []byte) error {
		received = append(received, string(chunk))
		return nil
	})
	require.NoError(t, err)
	require.Len(t, received, 2) // [DONE] skipped
	assert.True(t, strings.HasPrefix(received[0], "data: "))
}

func TestWatsonxAdapter_Forward_InvalidBaseURL(t *testing.T) {
	adapter := providers.NewWatsonxAdapter("not-watsonx://bad", "key")
	_, _, err := adapter.Forward(context.Background(), []byte(`{"model":"m","messages":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "watsonx:")
}

func TestWatsonxAdapter_BuildFilePrompt(t *testing.T) {
	adapter := providers.NewWatsonxAdapter("watsonx://us-south", "key")
	out := adapter.BuildFilePrompt("ibm/granite-13b-chat-v2", "doc text", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "ibm/granite-13b-chat-v2", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0].(map[string]interface{})["content"], "doc text")
}

func TestWatsonxAdapter_RegisteredInRegistry(t *testing.T) {
	adapter, err := providers.New("watsonx", "watsonx://us-south", "key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
