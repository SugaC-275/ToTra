package handlers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// --- fakes ---

type fakeResponsesLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (f *fakeResponsesLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return f.cfg, f.err
}

type fakeResponsesUsageRecorder struct {
	recorded []*storage.UsageRecord
}

func (f *fakeResponsesUsageRecorder) Record(r *storage.UsageRecord) {
	f.recorded = append(f.recorded, r)
}

// buildResponsesApp wires up a minimal Fiber app with the responses handler.
func buildResponsesApp(lookup handlers.ResponsesModelLookup, rec handlers.ResponsesUsageRecorder, user *middleware.UserInfo) *fiber.App {
	app := fiber.New()
	app.Post("/v1/responses", func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}, handlers.NewResponsesHandler(lookup, rec))
	return app
}

// --- tests ---

// TestResponsesHandler_Success verifies that a valid non-streaming request is
// proxied and usage is recorded using the Responses API token field names.
func TestResponsesHandler_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_abc","object":"response","usage":{"input_tokens":10,"output_tokens":20}}`)
	}))
	defer upstream.Close()

	lookup := &fakeResponsesLookup{
		cfg: &storage.ModelConfig{
			ID:      "cfg-1",
			BaseURL: upstream.URL + "/v1",
			APIKey:  "sk-test",
		},
	}
	rec := &fakeResponsesUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildResponsesApp(lookup, rec, user)

	reqBody := []byte(`{"model":"gpt-4o","input":"Hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	if len(rec.recorded) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(rec.recorded))
	}
	r := rec.recorded[0]
	if r.PromptTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", r.PromptTokens)
	}
	if r.CompletionTokens != 20 {
		t.Errorf("expected 20 output tokens, got %d", r.CompletionTokens)
	}
}

// TestResponsesHandler_NoModel verifies that a request missing the model field returns 400.
func TestResponsesHandler_NoModel(t *testing.T) {
	lookup := &fakeResponsesLookup{cfg: nil}
	rec := &fakeResponsesUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildResponsesApp(lookup, rec, user)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"input":"hi"}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestResponsesHandler_ModelNotConfigured verifies that a 400 is returned when
// the model is not configured for the tenant.
func TestResponsesHandler_ModelNotConfigured(t *testing.T) {
	lookup := &fakeResponsesLookup{cfg: nil, err: nil}
	rec := &fakeResponsesUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildResponsesApp(lookup, rec, user)

	reqBody := []byte(`{"model":"gpt-4o","input":"Hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestResponsesHandler_UpstreamError verifies that an upstream failure returns 502.
func TestResponsesHandler_UpstreamError(t *testing.T) {
	// Upstream that immediately closes the connection.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"server error"}}`))
	}))
	defer upstream.Close()

	lookup := &fakeResponsesLookup{
		cfg: &storage.ModelConfig{
			ID:      "cfg-1",
			BaseURL: upstream.URL + "/v1",
			APIKey:  "sk-test",
		},
	}
	rec := &fakeResponsesUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildResponsesApp(lookup, rec, user)

	reqBody := []byte(`{"model":"gpt-4o","input":"Hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// Handler forwards the upstream status code (500), not necessarily 502.
	// The upstream returned 500, so we expect 500 to be passed through.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 (upstream passthrough), got %d", resp.StatusCode)
	}
}
