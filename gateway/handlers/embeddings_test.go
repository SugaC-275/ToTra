package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
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

type fakeEmbeddingsLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (f *fakeEmbeddingsLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return f.cfg, f.err
}

type fakeEmbeddingsUsageRecorder struct {
	recorded []*storage.UsageRecord
}

func (f *fakeEmbeddingsUsageRecorder) Record(r *storage.UsageRecord) {
	f.recorded = append(f.recorded, r)
}

// buildEmbeddingsApp wires up a minimal Fiber app with the given handler.
func buildEmbeddingsApp(lookup handlers.EmbeddingsModelLookup, rec handlers.EmbeddingsUsageRecorder, user *middleware.UserInfo) *fiber.App {
	app := fiber.New()
	app.Post("/v1/embeddings", func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}, handlers.NewEmbeddingsHandler(lookup, rec))
	return app
}

// --- tests ---

// TestEmbeddingsHandler_Success verifies that a valid request is proxied to the
// upstream mock server and the response is forwarded verbatim.
func TestEmbeddingsHandler_Success(t *testing.T) {
	// Fake upstream OpenAI-compatible embeddings server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":5,"total_tokens":5}}`)
	}))
	defer upstream.Close()

	price := 0.02
	lookup := &fakeEmbeddingsLookup{
		cfg: &storage.ModelConfig{
			ID:             "cfg-1",
			Provider:       "openai",
			BaseURL:        upstream.URL + "/v1",
			APIKey:         "test-key",
			PricePerMInput: &price,
		},
	}
	rec := &fakeEmbeddingsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildEmbeddingsApp(lookup, rec, user)

	reqBody, _ := json.Marshal(map[string]any{
		"model": "text-embedding-ada-002",
		"input": "hello world",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))
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
	if rec.recorded[0].PromptTokens != 5 {
		t.Errorf("expected 5 prompt tokens, got %d", rec.recorded[0].PromptTokens)
	}
}

// TestEmbeddingsHandler_ModelNotFound verifies that a missing model returns 400.
func TestEmbeddingsHandler_ModelNotFound(t *testing.T) {
	lookup := &fakeEmbeddingsLookup{cfg: nil, err: nil}
	rec := &fakeEmbeddingsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildEmbeddingsApp(lookup, rec, user)

	reqBody, _ := json.Marshal(map[string]any{
		"model": "nonexistent-model",
		"input": "hello",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if errBody["error"] == nil {
		t.Error("expected error field in response body")
	}
}

// TestEmbeddingsHandler_MissingModel verifies that a request without a model field returns 400.
func TestEmbeddingsHandler_MissingModel(t *testing.T) {
	lookup := &fakeEmbeddingsLookup{}
	rec := &fakeEmbeddingsUsageRecorder{}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildEmbeddingsApp(lookup, rec, user)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader([]byte(`{"input":"hi"}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
