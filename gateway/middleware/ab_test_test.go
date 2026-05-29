package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

// mockABTestStore implements middleware.ABTestQuerier.
type mockABTestStore struct {
	entry *middleware.ABTestEntry
}

func (m *mockABTestStore) GetActiveForModel(_ context.Context, _, _ string) (*middleware.ABTestEntry, error) {
	return m.entry, nil
}

func newABTestApp(store middleware.ABTestQuerier, user *middleware.UserInfo) (*fiber.App, *string) {
	app := fiber.New()
	var capturedModel string
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Use(middleware.NewABTestMiddleware(store))
	app.Post("/test", func(c *fiber.Ctx) error {
		var req struct{ Model string `json:"model"` }
		_ = json.Unmarshal(c.Body(), &req)
		capturedModel = req.Model
		return c.SendStatus(200)
	})
	return app, &capturedModel
}

func TestABTestMiddleware_SplitDistribution(t *testing.T) {
	store := &mockABTestStore{entry: &middleware.ABTestEntry{
		ID:        "test-1",
		Name:      "exp-alpha",
		ModelA:    "gpt-4",
		ModelB:    "gpt-4o-mini",
		SplitPctB: 30,
	}}
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1"}
	app, captured := newABTestApp(store, user)

	const iterations = 1000
	bCount := 0
	for i := 0; i < iterations; i++ {
		body := `{"model":"gpt-4","messages":[]}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		if *captured == "gpt-4o-mini" {
			bCount++
		}
	}

	// Expect ~30% B. Allow ±10 percentage points (i.e. 200–400 out of 1000).
	pctB := float64(bCount) / float64(iterations) * 100
	assert.InDelta(t, 30.0, pctB, 10.0,
		"B variant chosen %.1f%% of the time; expected ~30%%", pctB)
}

func TestABTestMiddleware_VariantHeaders(t *testing.T) {
	// SplitPctB=100 forces B every time — deterministic for header checks.
	store := &mockABTestStore{entry: &middleware.ABTestEntry{
		ID: "test-2", Name: "exp-beta",
		ModelA: "gpt-4", ModelB: "gpt-4o-mini", SplitPctB: 100,
	}}
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1"}
	app, _ := newABTestApp(store, user)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, "exp-beta", resp.Header.Get("X-AB-Test"))
	assert.Equal(t, "B", resp.Header.Get("X-AB-Test-Variant"))
	assert.Equal(t, "gpt-4", resp.Header.Get("X-AB-Test-Original"))
	assert.Equal(t, "gpt-4o-mini", resp.Header.Get("X-AB-Test-Chosen"))
}

func TestABTestMiddleware_AlwaysAVariant(t *testing.T) {
	// SplitPctB=0 forces A every time.
	store := &mockABTestStore{entry: &middleware.ABTestEntry{
		ID: "test-3", Name: "exp-gamma",
		ModelA: "gpt-4", ModelB: "gpt-4o-mini", SplitPctB: 0,
	}}
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1"}
	app, captured := newABTestApp(store, user)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", *captured)
	assert.Equal(t, "A", resp.Header.Get("X-AB-Test-Variant"))
}

func TestABTestMiddleware_NilStorePassthrough(t *testing.T) {
	app, captured := newABTestApp(nil, &middleware.UserInfo{UserID: "u1", TenantID: "t1"})

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4", *captured)
}

func TestABTestMiddleware_NoActiveTest(t *testing.T) {
	store := &mockABTestStore{entry: nil} // simulates no active test
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1"}
	app, captured := newABTestApp(store, user)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4", *captured)
	assert.Empty(t, resp.Header.Get("X-AB-Test"))
}
