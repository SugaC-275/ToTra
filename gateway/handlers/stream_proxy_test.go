package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
)

// --- Fakes ---

type fakeModelLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (f *fakeModelLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return f.cfg, f.err
}

type fakeUsageRecorder struct {
	records []*storage.UsageRecord
}

func (f *fakeUsageRecorder) Record(r *storage.UsageRecord) {
	f.records = append(f.records, r)
}

type fakeAdapter struct {
	chunks []string
	err    error
}

func (f *fakeAdapter) Forward(_ context.Context, _ []byte) (*providers.ForwardResult, *providers.Usage, error) {
	return &providers.ForwardResult{StatusCode: 200, Body: []byte("{}")}, &providers.Usage{}, nil
}

func (f *fakeAdapter) ForwardStream(_ context.Context, _ []byte, onChunk func([]byte) error) error {
	for _, ch := range f.chunks {
		if err := onChunk([]byte(ch)); err != nil {
			return err
		}
	}
	return f.err
}

func (f *fakeAdapter) BuildFilePrompt(_, _, _ string) []byte { return nil }

// newTestApp wires up a minimal Fiber app with the stream proxy handler. It
// uses unexported constructor via handlers.ExportedForTest (see below).
func newTestStreamApp(t *testing.T, lookup handlers.StreamModelLookup, recorder handlers.UsageRecorder) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{UserID: "u1", TenantID: "t1"})
		return c.Next()
	})
	app.Post("/v1/chat/completions/stream", handlers.NewStreamProxyHandlerForTest(lookup, recorder))
	return app
}

func doRequest(t *testing.T, app *fiber.App, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/stream", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	require.NoError(t, err)
	return resp
}

// --- Tests ---

func TestStreamProxy_RejectsMissingStreamField(t *testing.T) {
	app := newTestStreamApp(t, &fakeModelLookup{}, &fakeUsageRecorder{})
	resp := doRequest(t, app, `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamProxy_RejectsStreamFalse(t *testing.T) {
	app := newTestStreamApp(t, &fakeModelLookup{}, &fakeUsageRecorder{})
	resp := doRequest(t, app, `{"model":"gpt-4o","stream":false,"messages":[]}`)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamProxy_RejectsMissingModel(t *testing.T) {
	app := newTestStreamApp(t, &fakeModelLookup{cfg: &storage.ModelConfig{ID: "m1", Provider: "openai"}}, &fakeUsageRecorder{})
	resp := doRequest(t, app, `{"stream":true}`)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamProxy_ModelNotFound(t *testing.T) {
	app := newTestStreamApp(t, &fakeModelLookup{cfg: nil}, &fakeUsageRecorder{})
	resp := doRequest(t, app, `{"model":"unknown","stream":true}`)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamProxy_StreamsChunksAndSSEHeaders(t *testing.T) {
	fa := &fakeAdapter{chunks: []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n",
	}}
	usageRec := &fakeUsageRecorder{}

	// Inject fake adapter via provider registry override.
	providers.Register("fake-stream", func(_, _ string) providers.Adapter { return fa })

	lookup := &fakeModelLookup{cfg: &storage.ModelConfig{
		ID: "m1", Provider: "fake-stream", BaseURL: "http://x", APIKey: "k",
	}}
	app := newTestStreamApp(t, lookup, usageRec)

	resp := doRequest(t, app, `{"model":"gpt-4o","stream":true,"messages":[]}`)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"content":"hello"`)
	assert.Contains(t, string(body), `"content":" world"`)
	assert.Len(t, usageRec.records, 1)
}

func TestStreamProxy_UpstreamErrorAfterStreamStart(t *testing.T) {
	fa := &fakeAdapter{
		chunks: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n"},
		err:    errors.New("upstream broke"),
	}
	providers.Register("fake-err-stream", func(_, _ string) providers.Adapter { return fa })

	lookup := &fakeModelLookup{cfg: &storage.ModelConfig{
		ID: "m2", Provider: "fake-err-stream", BaseURL: "http://x", APIKey: "k",
	}}
	usageRec := &fakeUsageRecorder{}
	app := newTestStreamApp(t, lookup, usageRec)

	resp := doRequest(t, app, `{"model":"gpt-4o","stream":true}`)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// partial content was delivered; error event appended
	assert.Contains(t, string(body), "partial")
	assert.Contains(t, string(body), "upstream broke")
	// usage still recorded even on error
	assert.Len(t, usageRec.records, 1)
}
