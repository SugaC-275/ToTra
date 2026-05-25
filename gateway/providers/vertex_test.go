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

// TestVertexBaseURLParsing validates the vertex:// URL parser.
func TestVertexBaseURLParsing(t *testing.T) {
	tests := []struct {
		baseURL       string
		wantRegion    string
		wantProjectID string
		wantErr       bool
	}{
		{
			baseURL:       "vertex://us-central1/my-gcp-project",
			wantRegion:    "us-central1",
			wantProjectID: "my-gcp-project",
		},
		{
			baseURL:       "vertex://europe-west4/prod-project-123",
			wantRegion:    "europe-west4",
			wantProjectID: "prod-project-123",
		},
		{
			baseURL:       "vertex://us-east4/company-prod-001",
			wantRegion:    "us-east4",
			wantProjectID: "company-prod-001",
		},
		// missing project component
		{baseURL: "vertex://us-central1", wantErr: true},
		// wrong scheme
		{baseURL: "https://us-central1/my-project", wantErr: true},
		// empty region
		{baseURL: "vertex:///my-project", wantErr: true},
		// empty project
		{baseURL: "vertex://us-central1/", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.baseURL, func(t *testing.T) {
			region, projectID, err := providers.ParseVertexBaseURL(tc.baseURL)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantRegion, region)
			assert.Equal(t, tc.wantProjectID, projectID)
		})
	}
}

// TestVertexBaseURLIsValid mirrors the bedrock boolean-helper pattern.
func TestVertexBaseURLIsValid(t *testing.T) {
	assert.True(t, providers.VertexBaseURLIsValid("vertex://us-central1/my-project"))
	assert.False(t, providers.VertexBaseURLIsValid("vertex://us-central1"))
	assert.False(t, providers.VertexBaseURLIsValid("https://us-central1/my-project"))
	assert.False(t, providers.VertexBaseURLIsValid("vertex:///my-project"))
}

// TestVertexEndpointConstruction validates the URL built from region, project, and model.
func TestVertexEndpointConstruction(t *testing.T) {
	tests := []struct {
		region    string
		projectID string
		model     string
		want      string
	}{
		{
			region:    "us-central1",
			projectID: "my-gcp-project",
			model:     "gemini-1.5-pro",
			want:      "https://us-central1-aiplatform.googleapis.com/v1/projects/my-gcp-project/locations/us-central1/publishers/google/models/gemini-1.5-pro:generateContent",
		},
		{
			region:    "europe-west4",
			projectID: "prod-project",
			model:     "gemini-2.0-flash",
			want:      "https://europe-west4-aiplatform.googleapis.com/v1/projects/prod-project/locations/europe-west4/publishers/google/models/gemini-2.0-flash:generateContent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.region+"/"+tc.model, func(t *testing.T) {
			got := providers.VertexEndpoint(tc.region, tc.projectID, tc.model)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestVertexAdapter_RegistryLookup confirms the adapter is registered under "vertex".
func TestVertexAdapter_RegistryLookup(t *testing.T) {
	adapter, err := providers.New("vertex", "vertex://us-central1/my-project", "")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestVertexAdapter_InvalidBaseURL checks that Forward returns an error for bad URLs.
func TestVertexAdapter_InvalidBaseURL(t *testing.T) {
	adapter := providers.NewVertexAdapter("not-a-vertex-url")
	_, _, err := adapter.Forward(context.Background(), []byte(`{"model":"gemini-1.5-pro"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vertex:")
}

// TestVertexAdapter_MissingModelField verifies the error when model field is absent.
func TestVertexAdapter_MissingModelField(t *testing.T) {
	adapter := providers.NewVertexAdapter("vertex://us-central1/my-project")
	_, _, err := adapter.Forward(context.Background(), []byte(`{"contents":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model field missing")
}

// TestVertexAdapter_Forward exercises the full HTTP round-trip with a fake upstream
// and injected Bearer token (no ADC required).
func TestVertexAdapter_Forward(t *testing.T) {
	var capturedAuth string
	var capturedPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}`))
	}))
	defer upstream.Close()

	adapter := providers.NewVertexAdapterWithClientAndToken(
		"us-central1", "my-project",
		upstream.Client(),
		upstream.URL,
		"fake-bearer-token",
	)

	body := []byte(`{"model":"gemini-1.5-pro","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	resp, usage, err := adapter.Forward(context.Background(), body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Bearer fake-bearer-token", capturedAuth)
	assert.Contains(t, capturedPath, "gemini-1.5-pro")
	assert.Equal(t, "us-central1", resp.Headers.Get("X-Vertex-Region"))
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
}

// TestVertexAdapter_ForwardStream exercises streaming with a fake SSE upstream.
func TestVertexAdapter_ForwardStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fake-stream-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.RawQuery, "alt=sse")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk1\"}]}}]}\n\n"))
		w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk2\"}]}}]}\n\n"))
	}))
	defer upstream.Close()

	adapter := providers.NewVertexAdapterWithClientAndToken(
		"us-central1", "my-project",
		upstream.Client(),
		upstream.URL,
		"fake-stream-token",
	)

	var chunks []string
	body := []byte(`{"model":"gemini-1.5-pro","contents":[{"role":"user","parts":[{"text":"stream me"}]}]}`)
	err := adapter.ForwardStream(context.Background(), body, func(chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, chunks, 2)
}

// TestVertexAdapter_BuildFilePrompt checks the Gemini-format document prompt shape.
func TestVertexAdapter_BuildFilePrompt(t *testing.T) {
	adapter := providers.NewVertexAdapter("vertex://us-central1/my-project")
	raw := adapter.BuildFilePrompt("gemini-1.5-pro", "doc text", "summarize this")

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "gemini-1.5-pro", got["model"])

	contents, ok := got["contents"].([]interface{})
	require.True(t, ok)
	require.Len(t, contents, 1)

	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	text := parts[0].(map[string]interface{})["text"].(string)
	assert.Contains(t, text, "doc text")
	assert.Contains(t, text, "summarize this")
}
