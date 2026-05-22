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

// mockRateLimitStore is a controllable stub for RateLimitChecker.
type mockRateLimitStore struct {
	tenantMax    int
	userMax      int
	configErr    error
	callCount    int
	denyAfter    int // deny starting at this call number (1-indexed, 0 = never)
	retryAfter   int
}

func (m *mockRateLimitStore) GetConfig(_ context.Context, _ string) (int, int, error) {
	return m.tenantMax, m.userMax, m.configErr
}

func (m *mockRateLimitStore) CheckAndIncrement(_ context.Context, _, _ string, limit int) (bool, int, int, error) {
	m.callCount++
	if m.denyAfter > 0 && m.callCount >= m.denyAfter {
		return false, 0, m.retryAfter, nil
	}
	return true, limit - 1, 0, nil
}

func setupRateLimiterApp(store middleware.RateLimitChecker, injectUser bool) *fiber.App {
	app := fiber.New()
	if injectUser {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", &middleware.UserInfo{UserID: "u1", TenantID: "t1", Role: "standard"})
			return c.Next()
		})
	}
	app.Use(middleware.NewRateLimiterMiddleware(store))
	app.Get("/ping", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	return app
}

func TestRateLimiterMiddleware_Allowed(t *testing.T) {
	store := &mockRateLimitStore{tenantMax: 60, userMax: 20}
	app := setupRateLimiterApp(store, true)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Remaining"))
}

func TestRateLimiterMiddleware_NoUser_PassThrough(t *testing.T) {
	store := &mockRateLimitStore{tenantMax: 60, userMax: 20}
	app := setupRateLimiterApp(store, false)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	// No user → middleware skips → handler responds 200.
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, store.callCount, "CheckAndIncrement must not be called without a user")
}

func TestRateLimiterMiddleware_TenantLimitExceeded(t *testing.T) {
	// denyAfter=1 means the first CheckAndIncrement call (tenant check) is denied.
	store := &mockRateLimitStore{tenantMax: 10, userMax: 5, denyAfter: 1, retryAfter: 30}
	app := setupRateLimiterApp(store, true)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 429, resp.StatusCode)
	assert.Equal(t, "30", resp.Header.Get("Retry-After"))
}

func TestRateLimiterMiddleware_UserLimitExceeded(t *testing.T) {
	// denyAfter=2 means the second CheckAndIncrement call (user check) is denied.
	store := &mockRateLimitStore{tenantMax: 10, userMax: 5, denyAfter: 2, retryAfter: 45}
	app := setupRateLimiterApp(store, true)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 429, resp.StatusCode)
	assert.Equal(t, "45", resp.Header.Get("Retry-After"))
}

func TestRateLimiterMiddleware_ResponseHeaders(t *testing.T) {
	store := &mockRateLimitStore{tenantMax: 60, userMax: 20}
	app := setupRateLimiterApp(store, true)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, "20", resp.Header.Get("X-RateLimit-Limit"))
}
