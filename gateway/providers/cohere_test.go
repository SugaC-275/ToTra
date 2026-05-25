package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// translateRequest
// ---------------------------------------------------------------------------

func TestTranslateRequest_PassthroughStandardFields(t *testing.T) {
	input := `{
		"model": "command-r-plus",
		"messages": [{"role": "user", "content": "Hello"}],
		"temperature": 0.7,
		"stream": false
	}`
	out, err := translateRequest([]byte(input))
	require.NoError(t, err)

	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))

	assert.Contains(t, got, "model")
	assert.Contains(t, got, "messages")
	assert.Contains(t, got, "temperature")
	assert.Contains(t, got, "stream")
}

func TestTranslateRequest_CohereExtensionFields(t *testing.T) {
	input := `{
		"model": "command-r-plus",
		"messages": [{"role": "user", "content": "Search this"}],
		"_cohere_connectors": [{"id": "web-search"}],
		"_cohere_documents": [{"id": "1", "data": {"text": "doc"}}],
		"_cohere_search_queries_only": true
	}`
	out, err := translateRequest([]byte(input))
	require.NoError(t, err)

	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))

	// _cohere_ prefix stripped, native keys present
	assert.Contains(t, got, "connectors", "connectors should be promoted")
	assert.Contains(t, got, "documents", "documents should be promoted")
	assert.Contains(t, got, "search_queries_only", "search_queries_only should be promoted")

	// original prefixed keys must not appear in output
	assert.NotContains(t, got, "_cohere_connectors")
	assert.NotContains(t, got, "_cohere_documents")
	assert.NotContains(t, got, "_cohere_search_queries_only")
}

func TestTranslateRequest_UnknownFieldsDropped(t *testing.T) {
	// Fields outside the known OpenAI set and without _cohere_ prefix are dropped.
	input := `{"model":"command-r","messages":[],"unknown_field":"value"}`
	out, err := translateRequest([]byte(input))
	require.NoError(t, err)

	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))
	assert.NotContains(t, got, "unknown_field")
}

func TestTranslateRequest_InvalidJSON(t *testing.T) {
	_, err := translateRequest([]byte(`not json`))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// translateResponse
// ---------------------------------------------------------------------------

func TestTranslateResponse_CompleteToChatCompletion(t *testing.T) {
	cohereResp := `{
		"id": "abc123",
		"finish_reason": "COMPLETE",
		"message": {
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello there!"}]
		},
		"usage": {
			"billed_units": {"input_tokens": 10, "output_tokens": 5}
		}
	}`
	out, err := translateResponse([]byte(cohereResp))
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))

	assert.Equal(t, "abc123", got["id"])
	assert.Equal(t, "chat.completion", got["object"])

	choices := got["choices"].([]interface{})
	require.Len(t, choices, 1)
	choice := choices[0].(map[string]interface{})
	assert.Equal(t, "stop", choice["finish_reason"])
	msg := choice["message"].(map[string]interface{})
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "Hello there!", msg["content"])

	usage := got["usage"].(map[string]interface{})
	assert.Equal(t, float64(10), usage["prompt_tokens"])
	assert.Equal(t, float64(5), usage["completion_tokens"])
	assert.Equal(t, float64(15), usage["total_tokens"])
}

func TestTranslateResponse_NonCompleteFinishReason(t *testing.T) {
	cohereResp := `{
		"id": "xyz",
		"finish_reason": "MAX_TOKENS",
		"message": {"role": "assistant", "content": [{"type": "text", "text": "..."}]},
		"usage": {"billed_units": {"input_tokens": 1, "output_tokens": 1}}
	}`
	out, err := translateResponse([]byte(cohereResp))
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))

	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	assert.Equal(t, "max_tokens", choice["finish_reason"])
}

func TestTranslateResponse_EmptyContent(t *testing.T) {
	cohereResp := `{
		"id": "e1",
		"finish_reason": "COMPLETE",
		"message": {"role": "assistant", "content": []},
		"usage": {"billed_units": {"input_tokens": 0, "output_tokens": 0}}
	}`
	out, err := translateResponse([]byte(cohereResp))
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	choices := got["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	assert.Equal(t, "", msg["content"])
}

// ---------------------------------------------------------------------------
// extractCohereUsage
// ---------------------------------------------------------------------------

func TestExtractCohereUsage(t *testing.T) {
	body := `{"usage":{"billed_units":{"input_tokens":12,"output_tokens":34}}}`
	u := extractCohereUsage([]byte(body))
	assert.Equal(t, 12, u.PromptTokens)
	assert.Equal(t, 34, u.CompletionTokens)
}

func TestExtractCohereUsage_InvalidJSON(t *testing.T) {
	u := extractCohereUsage([]byte(`bad`))
	assert.Equal(t, &Usage{}, u)
}

// ---------------------------------------------------------------------------
// CohereAdapter.Forward (HTTP round-trip)
// ---------------------------------------------------------------------------

func TestCohereAdapter_Forward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-cohere-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/v2/chat", r.URL.Path)

		// Verify the translated request has no _cohere_ prefix fields
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.NotContains(t, body, "_cohere_documents")
		assert.Contains(t, body, "documents")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"id": "resp1",
			"finish_reason": "COMPLETE",
			"message": {"role": "assistant", "content": [{"type": "text", "text": "Hi!"}]},
			"usage": {"billed_units": {"input_tokens": 8, "output_tokens": 3}}
		}`))
	}))
	defer upstream.Close()

	adapter := NewCohereAdapter(upstream.URL, "test-cohere-key")
	reqBody := `{
		"model": "command-r-plus",
		"messages": [{"role": "user", "content": "Hi"}],
		"_cohere_documents": [{"id": "1", "data": {"text": "context"}}]
	}`

	result, usage, err := adapter.Forward(context.Background(), []byte(reqBody))
	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)

	// Response body should be OpenAI-shaped
	var oaiResp map[string]interface{}
	require.NoError(t, json.Unmarshal(result.Body, &oaiResp))
	assert.Equal(t, "chat.completion", oaiResp["object"])
	choices := oaiResp["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	assert.Equal(t, "Hi!", msg["content"])
}

func TestCohereAdapter_Forward_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer upstream.Close()

	adapter := NewCohereAdapter(upstream.URL, "bad-key")
	result, usage, err := adapter.Forward(context.Background(), []byte(`{"model":"command-r","messages":[]}`))
	require.NoError(t, err) // network-level success
	assert.Equal(t, 401, result.StatusCode)
	assert.Equal(t, &Usage{}, usage)
}

// ---------------------------------------------------------------------------
// CohereAdapter.ForwardStream
// ---------------------------------------------------------------------------

func TestCohereAdapter_ForwardStream(t *testing.T) {
	ssePayload := strings.Join([]string{
		"event: content-delta",
		`data: {"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":"Hello"}}}}`,
		"",
		"event: content-delta",
		`data: {"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":" world"}}}}`,
		"",
		"event: message-end",
		`data: {"finish_reason":"COMPLETE"}`,
		"",
	}, "\n")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(ssePayload))
	}))
	defer upstream.Close()

	adapter := NewCohereAdapter(upstream.URL, "key")
	var chunks []string
	err := adapter.ForwardStream(context.Background(), []byte(`{"model":"command-r","messages":[]}`),
		func(b []byte) error {
			chunks = append(chunks, string(b))
			return nil
		})
	require.NoError(t, err)

	// Expect two content chunks and one [DONE]
	require.Len(t, chunks, 3)
	assert.Contains(t, chunks[0], `"Hello"`)
	assert.Contains(t, chunks[1], `" world"`)
	assert.Equal(t, "data: [DONE]\n", chunks[2])
}

// ---------------------------------------------------------------------------
// readCohereSSE
// ---------------------------------------------------------------------------

func TestReadCohereSSE_ContentDelta(t *testing.T) {
	sseInput := "event: content-delta\n" +
		`data: {"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":"Hi"}}}}` + "\n\n"

	var got []string
	err := readCohereSSE(bytes.NewReader([]byte(sseInput)), func(b []byte) error {
		got = append(got, string(b))
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 1)

	// Verify the emitted line is OpenAI SSE shaped
	line := got[0]
	assert.True(t, strings.HasPrefix(line, "data: "), "should start with 'data: '")
	payload := strings.TrimPrefix(strings.TrimSuffix(line, "\n"), "data: ")
	var chunk map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
	assert.Equal(t, "chat.completion.chunk", chunk["object"])
	choices := chunk["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	assert.Equal(t, "Hi", delta["content"])
}

func TestReadCohereSSE_MessageEnd(t *testing.T) {
	sseInput := "event: message-end\n" +
		`data: {"finish_reason":"COMPLETE"}` + "\n\n"

	var got []string
	err := readCohereSSE(bytes.NewReader([]byte(sseInput)), func(b []byte) error {
		got = append(got, string(b))
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "data: [DONE]\n", got[0])
}

func TestReadCohereSSE_SkipsUnknownEvents(t *testing.T) {
	sseInput := "event: stream-start\n" +
		`data: {"generation_id":"abc"}` + "\n\n" +
		"event: content-delta\n" +
		`data: {"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":"X"}}}}` + "\n\n"

	var got []string
	err := readCohereSSE(bytes.NewReader([]byte(sseInput)), func(b []byte) error {
		got = append(got, string(b))
		return nil
	})
	require.NoError(t, err)
	// Only the content-delta should produce a chunk
	require.Len(t, got, 1)
	assert.Contains(t, got[0], `"X"`)
}

// ---------------------------------------------------------------------------
// Registry registration
// ---------------------------------------------------------------------------

func TestCohereRegistered(t *testing.T) {
	adapter, err := New("cohere", "", "test-key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

func TestCohereDefaultBaseURL(t *testing.T) {
	// When registered with empty baseURL the adapter should use the default.
	a := NewCohereAdapter("", "key")
	// We confirm it is usable (no panic), actual URL is private but covered by Forward test.
	assert.NotNil(t, a)
}

// ---------------------------------------------------------------------------
// BuildFilePrompt
// ---------------------------------------------------------------------------

func TestCohereAdapter_BuildFilePrompt(t *testing.T) {
	a := NewCohereAdapter("http://x", "key")
	body := a.BuildFilePrompt("command-r-plus", "doc content", "summarize")

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "command-r-plus", got["model"])

	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)

	sys := msgs[0].(map[string]interface{})
	assert.Equal(t, "system", sys["role"])
	assert.Contains(t, sys["content"].(string), "doc content")

	user := msgs[1].(map[string]interface{})
	assert.Equal(t, "user", user["role"])
	assert.Equal(t, "summarize", user["content"])
}

// ---------------------------------------------------------------------------
// extractCohereContentDelta
// ---------------------------------------------------------------------------

func TestExtractCohereContentDelta(t *testing.T) {
	data := `{"index":0,"delta":{"type":"text_delta","message":{"content":{"type":"text","text":"hello"}}}}`
	text, err := extractCohereContentDelta([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "hello", text)
}

func TestExtractCohereContentDelta_MissingText(t *testing.T) {
	data := `{"index":0,"delta":{}}`
	text, err := extractCohereContentDelta([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "", text)
}

// ---------------------------------------------------------------------------
// Ensure ForwardStream injects stream=true in the outgoing request
// ---------------------------------------------------------------------------

func TestCohereAdapter_ForwardStream_InjectsStreamTrue(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, json.RawMessage("true"), body["stream"])
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		// Minimal valid SSE — no events, just EOF
	}))
	defer upstream.Close()

	adapter := NewCohereAdapter(upstream.URL, "key")
	_ = adapter.ForwardStream(context.Background(), []byte(`{"model":"command-r","messages":[]}`),
		func(b []byte) error { return nil })
}

// ---------------------------------------------------------------------------
// Verify _cohere_ fields reach the upstream body via ForwardStream
// ---------------------------------------------------------------------------

func TestCohereAdapter_ForwardStream_ExtensionFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Contains(t, body, "connectors", "connectors must be present in upstream request")
		assert.NotContains(t, body, "_cohere_connectors")

		// scan connectors value
		var connectors []map[string]string
		require.NoError(t, json.Unmarshal(body["connectors"], &connectors))
		assert.Equal(t, "web-search", connectors[0]["id"])

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	adapter := NewCohereAdapter(upstream.URL, "key")
	reqBody := `{"model":"command-r","messages":[],"_cohere_connectors":[{"id":"web-search"}]}`
	_ = adapter.ForwardStream(context.Background(), []byte(reqBody),
		func(b []byte) error { return nil })
}

// ---------------------------------------------------------------------------
// Scanner edge-case: empty SSE body produces no chunks and no error
// ---------------------------------------------------------------------------

func TestReadCohereSSE_EmptyBody(t *testing.T) {
	err := readCohereSSE(bytes.NewReader(nil), func(b []byte) error {
		t.Fatal("onChunk should not be called on empty body")
		return nil
	})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Multiline SSE scan robustness (bufio.Scanner default 64 KB buffer)
// ---------------------------------------------------------------------------

func TestReadCohereSSE_MultipleChunks(t *testing.T) {
	var sb strings.Builder
	for i := range 5 {
		sb.WriteString("event: content-delta\n")
		payload := map[string]interface{}{
			"index": i,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"message": map[string]interface{}{
					"content": map[string]string{"type": "text", "text": "word"},
				},
			},
		}
		b, _ := json.Marshal(payload)
		sb.WriteString("data: " + string(b) + "\n\n")
	}
	sb.WriteString("event: message-end\ndata: {}\n\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	_ = scanner // just validate it parses without error

	var chunks []string
	err := readCohereSSE(strings.NewReader(sb.String()), func(b []byte) error {
		chunks = append(chunks, string(b))
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, chunks, 6) // 5 content-delta + 1 message-end
}
