package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// stubVideoLookup implements VideoModelLookup for tests.
type stubVideoLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (s *stubVideoLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return s.cfg, s.err
}

// stubVideoRecorder implements VideoUsageRecorder for tests.
type stubVideoRecorder struct{ recorded []*storage.UsageRecord }

func (s *stubVideoRecorder) Record(r *storage.UsageRecord) { s.recorded = append(s.recorded, r) }

func newVideoTestApp(lookup VideoModelLookup, recorder VideoUsageRecorder) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Post("/v1/video/generations", NewVideoGenerationHandler(lookup, recorder))
	return app
}

func TestVideoHandler_MissingModel(t *testing.T) {
	app := newVideoTestApp(&stubVideoLookup{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{"prompt":"a bird"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "model field required") {
		t.Errorf("body should mention 'model field required', got: %s", body)
	}
}

func TestVideoHandler_EmptyBody(t *testing.T) {
	app := newVideoTestApp(&stubVideoLookup{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestVideoHandler_Unauthorized(t *testing.T) {
	app := fiber.New()
	app.Post("/v1/video/generations", NewVideoGenerationHandler(&stubVideoLookup{}, nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{"model":"gen3a_turbo","prompt":"test"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestVideoHandler_ModelNotFound(t *testing.T) {
	lookup := &stubVideoLookup{cfg: nil, err: nil}
	app := newVideoTestApp(lookup, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{"model":"gen3a_turbo"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "model_not_found") {
		t.Errorf("body should mention 'model_not_found', got: %s", body)
	}
}

func TestVideoHandler_UnsupportedProvider(t *testing.T) {
	lookup := &stubVideoLookup{cfg: &storage.ModelConfig{
		ID:       "m1",
		Provider: "nonexistent_provider_xyz",
		APIKey:   "key",
		BaseURL:  "https://example.com",
	}}
	app := newVideoTestApp(lookup, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{"model":"some-model"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "unsupported_provider") {
		t.Errorf("body should mention 'unsupported_provider', got: %s", body)
	}
}
