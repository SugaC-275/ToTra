package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func init() {
	Register("vertex", func(baseURL, _ string) Adapter {
		return NewVertexAdapter(baseURL)
	})
}

// VertexAdapter forwards requests to Google Vertex AI using Application Default Credentials.
// baseURL format: "vertex://{region}/{project_id}"
// e.g. "vertex://us-central1/my-gcp-project"
type VertexAdapter struct {
	region    string
	projectID string
	client    *http.Client

	// staticToken, when non-empty, is used instead of ADC (for tests).
	staticToken string

	mu          sync.RWMutex
	cachedToken *oauth2.Token
}

// parseVertexBaseURL parses a vertex:// URL into region and project ID.
func parseVertexBaseURL(baseURL string) (region, projectID string, err error) {
	path := strings.TrimPrefix(baseURL, "vertex://")
	if path == baseURL {
		return "", "", fmt.Errorf("vertex: baseURL must start with vertex://, got %q", baseURL)
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("vertex: baseURL must be vertex://{region}/{project_id}, got %q", baseURL)
	}
	return parts[0], parts[1], nil
}

// VertexBaseURLIsValid returns true if the baseURL is a valid vertex:// URL.
// Exported for unit testing.
func VertexBaseURLIsValid(baseURL string) bool {
	_, _, err := parseVertexBaseURL(baseURL)
	return err == nil
}

// ParseVertexBaseURL parses a vertex:// URL and returns the region and project ID.
// Exported for unit testing.
func ParseVertexBaseURL(baseURL string) (region, projectID string, err error) {
	return parseVertexBaseURL(baseURL)
}

// VertexEndpoint returns the Vertex AI generateContent URL for a given model.
// Exported for unit testing.
func VertexEndpoint(region, projectID, model string) string {
	return vertexEndpoint(region, projectID, model)
}

func vertexEndpoint(region, projectID, model string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		region, projectID, region, model,
	)
}

func vertexStreamEndpoint(region, projectID, model string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
		region, projectID, region, model,
	)
}

// NewVertexAdapter creates a VertexAdapter using Application Default Credentials.
func NewVertexAdapter(baseURL string) *VertexAdapter {
	region, projectID, err := parseVertexBaseURL(baseURL)
	if err != nil {
		return &VertexAdapter{client: &http.Client{}}
	}
	return &VertexAdapter{
		region:    region,
		projectID: projectID,
		client:    &http.Client{},
	}
}

// NewVertexAdapterForTest creates a VertexAdapter with an injected HTTP client
// and a static Bearer token, bypassing ADC. Requests are sent to overrideBaseURL
// instead of the real Vertex AI endpoint (so tests can point at httptest.Server).
// Exported for use in tests only.
func NewVertexAdapterForTest(region, projectID string, client *http.Client, overrideBaseURL, staticToken string) *VertexAdapter {
	return &VertexAdapter{
		region:      region,
		projectID:   projectID,
		client:      client,
		staticToken: staticToken,
		// Store overrideBaseURL as a sentinel in projectID? No — use a redirect transport.
		// We embed the override in the client's Transport instead.
	}
}

// redirectTransport rewrites all requests so the scheme+host is replaced by
// a fixed target base URL. Used in tests to redirect Vertex calls to httptest.
type redirectTransport struct {
	base      string
	transport http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace scheme+host with the test base.
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(t.base, "http://")
	clone.Host = clone.URL.Host
	return t.transport.RoundTrip(clone)
}

// NewVertexAdapterWithClientAndToken creates a VertexAdapter that sends all
// requests to overrideBaseURL (e.g. an httptest.Server URL) with a static token.
// Intended for unit tests only.
func NewVertexAdapterWithClientAndToken(region, projectID string, _ *http.Client, overrideBaseURL, staticToken string) *VertexAdapter {
	transport := &redirectTransport{
		base:      overrideBaseURL,
		transport: http.DefaultTransport,
	}
	return &VertexAdapter{
		region:      region,
		projectID:   projectID,
		client:      &http.Client{Transport: transport},
		staticToken: staticToken,
	}
}

// getToken returns a valid Bearer token. Uses staticToken when set (tests),
// otherwise fetches from Application Default Credentials with caching.
func (a *VertexAdapter) getToken(ctx context.Context) (string, error) {
	if a.staticToken != "" {
		return a.staticToken, nil
	}

	a.mu.RLock()
	tok := a.cachedToken
	a.mu.RUnlock()

	if tok != nil && tok.Valid() {
		return tok.AccessToken, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock.
	if a.cachedToken != nil && a.cachedToken.Valid() {
		return a.cachedToken.AccessToken, nil
	}

	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("vertex: find ADC credentials: %w", err)
	}

	newTok, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("vertex: obtain ADC token: %w", err)
	}

	// Shrink the expiry by 30 s to refresh before actual expiry.
	if !newTok.Expiry.IsZero() {
		newTok.Expiry = newTok.Expiry.Add(-30 * time.Second)
	}

	a.cachedToken = newTok
	return newTok.AccessToken, nil
}

// extractModel returns the "model" field from a JSON request body.
func extractModel(body []byte) (string, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		return "", fmt.Errorf("vertex: model field missing in request body")
	}
	return req.Model, nil
}

// Forward sends the request to Vertex AI and returns the raw response.
func (a *VertexAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	if a.region == "" || a.projectID == "" {
		return nil, nil, fmt.Errorf("vertex: invalid baseURL — region and project_id must be non-empty")
	}

	model, err := extractModel(body)
	if err != nil {
		return nil, nil, err
	}

	token, err := a.getToken(ctx)
	if err != nil {
		return nil, nil, err
	}

	url := vertexEndpoint(a.region, a.projectID, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("vertex: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("vertex: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("vertex: read response: %w", err)
	}

	headers := resp.Header.Clone()
	headers.Set("X-Vertex-Region", a.region)

	return &ForwardResult{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       respBody,
	}, extractGeminiUsage(respBody), nil
}

// ForwardStream sends the request to Vertex AI via SSE streaming and delivers
// each SSE data line to onChunk.
func (a *VertexAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	if a.region == "" || a.projectID == "" {
		return fmt.Errorf("vertex: invalid baseURL — region and project_id must be non-empty")
	}

	model, err := extractModel(body)
	if err != nil {
		return err
	}

	token, err := a.getToken(ctx)
	if err != nil {
		return err
	}

	url := vertexStreamEndpoint(a.region, a.projectID, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("vertex: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("vertex: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vertex: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

// BuildFilePrompt builds a Vertex AI (Gemini-format) request body for document Q&A.
func (a *VertexAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"contents": []map[string]interface{}{
			{"role": "user", "parts": []map[string]string{
				{"text": "以下是用户上传的文档内容：\n\n" + docText + "\n\n" + userMessage},
			}},
		},
	}
	b, _ := json.Marshal(body)
	return b
}
