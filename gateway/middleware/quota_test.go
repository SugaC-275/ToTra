package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type mockQuotaStore struct {
	allowed   bool
	remaining int
}

func (m *mockQuotaStore) CheckAndIncrement(_ context.Context, _, _, _ string, _, _ int) (bool, int, error) {
	return m.allowed, m.remaining, nil
}

type mockUserQuota struct {
	quotaSCU int
}

func (m *mockUserQuota) GetUserQuota(_ context.Context, _, _ string) (int, error) {
	return m.quotaSCU, nil
}

func setupQuotaApp(allowed bool, remaining int) *fiber.App {
	app := fiber.New()
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1", Role: "standard"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	qs := &mockQuotaStore{allowed: allowed, remaining: remaining}
	uq := &mockUserQuota{quotaSCU: 1000}
	app.Use(middleware.NewQuotaMiddleware(qs, uq))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func TestQuotaMiddleware_Allowed(t *testing.T) {
	app := setupQuotaApp(true, 900)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "900", resp.Header.Get("X-RateLimit-Remaining"))
}

func TestQuotaMiddleware_Exceeded(t *testing.T) {
	app := setupQuotaApp(false, 0)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 429, resp.StatusCode)
}
