package handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// --- fakes ---

type fakeAssistantsLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (f *fakeAssistantsLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return f.cfg, f.err
}

type fakeAssistantsUsageRecorder struct {
	recorded []*storage.UsageRecord
}

func (f *fakeAssistantsUsageRecorder) Record(r *storage.UsageRecord) {
	f.recorded = append(f.recorded, r)
}

// buildAssistantsApp wires up a Fiber app with the assistants routes.
func buildAssistantsApp(lookup handlers.AssistantsModelLookup, rec handlers.AssistantsUsageRecorder, user *middleware.UserInfo) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1", func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	handlers.RegisterAssistantsRoutes(v1, lookup, rec, nil, nil)
	return app
}

// --- tests ---

// TestAssistantsHandler_NoModelConfigured verifies that a 400 is returned when the
// tenant has no model configured for the default assistants model.
func TestAssistantsHandler_NoModelConfigured(t *testing.T) {
	lookup := &fakeAssistantsLookup{cfg: nil, err: nil}
	rec := &fakeAssistantsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildAssistantsApp(lookup, rec, user)

	req := httptest.NewRequest(http.MethodPost, "/v1/assistants", bytes.NewReader([]byte(`{"instructions":"You are a helpful assistant."}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestAssistantsHandler_ModelFromBody verifies that the model field in the body
// is used for the lookup, and that the upstream response is forwarded.
func TestAssistantsHandler_ModelFromBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"run_abc","object":"thread.run","status":"queued"}`))
	}))
	defer upstream.Close()

	lookup := &fakeAssistantsLookup{
		cfg: &storage.ModelConfig{
			ID:      "cfg-1",
			BaseURL: upstream.URL + "/v1",
			APIKey:  "sk-test",
		},
	}
	rec := &fakeAssistantsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildAssistantsApp(lookup, rec, user)

	body := []byte(`{"model":"gpt-4o","instructions":"Do something."}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/threads/thread_123/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(rec.recorded) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(rec.recorded))
	}
}

// TestAssistantsHandler_ListAssistants verifies GET /v1/assistants is proxied.
func TestAssistantsHandler_ListAssistants(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer upstream.Close()

	lookup := &fakeAssistantsLookup{
		cfg: &storage.ModelConfig{
			ID:      "cfg-1",
			BaseURL: upstream.URL + "/v1",
			APIKey:  "sk-test",
		},
	}
	rec := &fakeAssistantsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildAssistantsApp(lookup, rec, user)

	req := httptest.NewRequest(http.MethodGet, "/v1/assistants", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
