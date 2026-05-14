package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type captureRecorder struct{ events []middleware.RoutingEvent }

func (r *captureRecorder) Record(_ context.Context, e middleware.RoutingEvent) {
	r.events = append(r.events, e)
}

func setupRouterApp(rec middleware.RoutingRecorder) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })
	return app
}

func TestAutoRouter_PremiumModel_ShortBody_Downgraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, len(rec.events))
	assert.Equal(t, "claude-opus-4-7", rec.events[0].OriginalModel)
	assert.Equal(t, "claude-sonnet-4-6", rec.events[0].RoutedModel)
}

func TestAutoRouter_StandardModel_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events))
}

func TestAutoRouter_PremiumModel_LongBody_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	longContent := strings.Repeat("analyze this carefully: ", 20)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"` + longContent + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events))
}

func TestAutoRouter_NilRecorder_NoPanic(t *testing.T) {
	app := setupRouterApp(nil)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
