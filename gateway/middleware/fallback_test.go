package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)

// mockFallbackLookup implements middleware.FallbackLookup for tests.
type mockFallbackLookup struct {
	fallbacks map[string]*middleware.ModelConfig // key: primaryModelName
	err       error
}

func (m *mockFallbackLookup) GetFallbackModel(_ context.Context, _, primaryModelName string) (*middleware.ModelConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	cfg, ok := m.fallbacks[primaryModelName]
	if !ok {
		return nil, nil
	}
	return cfg, nil
}

// proxyHandlerStub simulates the upstream proxy: returns the configured status code
// and records the model name seen in the request body.
type proxyHandlerStub struct {
	statusCode int
	seenModel  string
}

func (p *proxyHandlerStub) handler(c *fiber.Ctx) error {
	var req struct {
		Model string `json:"model"`
	}
	_ = c.BodyParser(&req)
	p.seenModel = req.Model
	return c.Status(p.statusCode).JSON(fiber.Map{"stub": true})
}

// setupFallbackApp wires auth stub → primary proxy → fallback middleware.
// The primary proxy is represented by firstHandler (runs for the initial request)
// and fallbackProxy (runs if fallback is triggered).
func setupFallbackApp(
	lookup middleware.FallbackLookup,
	primaryStatus int,
	fallbackProxy *proxyHandlerStub,
) (*fiber.App, *proxyHandlerStub) {
	primary := &proxyHandlerStub{statusCode: primaryStatus}

	app := fiber.New()

	// Inject user into Locals so the middleware can find it.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tenant-1", UserID: "user-1"})
		return c.Next()
	})

	// Fallback middleware wraps the fallback proxy handler.
	app.Use(middleware.NewFallbackMiddleware(lookup, fallbackProxy.handler))

	// Primary proxy handler (registered as the actual route handler).
	app.Post("/v1/chat/completions", primary.handler)

	return app, primary
}

func makeBody(model string) string {
	return `{"model":"` + model + `","messages":[{"role":"user","content":"hello"}]}`
}

func postJSON(app *fiber.App, body string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, 5000)
	return resp
}

// TestFallbackMiddleware_NoFallbackOnSuccess checks that 200 responses pass through untouched.
func TestFallbackMiddleware_NoFallbackOnSuccess(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"gpt-4o": {ID: "fb-1", Name: "gpt-4o-mini"},
	}}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app, primary := setupFallbackApp(lookup, http.StatusOK, fallbackProxy)

	resp := postJSON(app, makeBody("gpt-4o"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gpt-4o", primary.seenModel)
	assert.Equal(t, "", fallbackProxy.seenModel, "fallback proxy must not be called on success")
}

// TestFallbackMiddleware_FallbackOn502 checks that 502 triggers fallback model substitution.
func TestFallbackMiddleware_FallbackOn502(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"gpt-4o": {ID: "fb-1", Name: "gpt-4o-mini"},
	}}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app, _ := setupFallbackApp(lookup, http.StatusBadGateway, fallbackProxy)

	resp := postJSON(app, makeBody("gpt-4o"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gpt-4o-mini", fallbackProxy.seenModel, "fallback proxy must see the fallback model name")
}

// TestFallbackMiddleware_FallbackOn503 checks the same behaviour for 503.
func TestFallbackMiddleware_FallbackOn503(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"primary-model": {ID: "fb-2", Name: "fallback-model"},
	}}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app, _ := setupFallbackApp(lookup, http.StatusServiceUnavailable, fallbackProxy)

	resp := postJSON(app, makeBody("primary-model"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "fallback-model", fallbackProxy.seenModel)
}

// TestFallbackMiddleware_NoFallbackConfigured checks that 502 without a configured fallback
// propagates the original error status.
func TestFallbackMiddleware_NoFallbackConfigured(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{}} // empty
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app, _ := setupFallbackApp(lookup, http.StatusBadGateway, fallbackProxy)

	resp := postJSON(app, makeBody("gpt-4o"))
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "", fallbackProxy.seenModel, "fallback proxy must not run when no fallback is configured")
}

// TestFallbackMiddleware_LookupError does not panic and returns original status.
func TestFallbackMiddleware_LookupError(t *testing.T) {
	lookup := &mockFallbackLookup{err: context.DeadlineExceeded}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app, _ := setupFallbackApp(lookup, http.StatusBadGateway, fallbackProxy)

	resp := postJSON(app, makeBody("gpt-4o"))
	// Original 502 passes through when lookup errors — no panic.
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

// TestFallbackMiddleware_LocalsSet checks that fallback_used and fallback_model_name are set.
func TestFallbackMiddleware_LocalsSet(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"gpt-4o": {ID: "fb-1", Name: "gpt-4o-mini"},
	}}

	var capturedUsed interface{}
	var capturedName interface{}

	// Custom fallback proxy that captures locals.
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})

	app.Use(middleware.NewFallbackMiddleware(lookup, func(c *fiber.Ctx) error {
		capturedUsed = c.Locals("fallback_used")
		capturedName = c.Locals("fallback_model_name")
		return c.Status(http.StatusOK).JSON(fiber.Map{"ok": true})
	}))

	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.Status(http.StatusBadGateway).JSON(fiber.Map{"error": "upstream down"})
	})

	resp := postJSON(app, makeBody("gpt-4o"))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, true, capturedUsed)
	assert.Equal(t, "gpt-4o-mini", capturedName)
}

// TestFallbackMiddleware_NoUserInLocals does not panic when auth middleware is absent.
func TestFallbackMiddleware_NoUserInLocals(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"gpt-4o": {ID: "fb-1", Name: "gpt-4o-mini"},
	}}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	app := fiber.New()
	// No user injected into Locals.
	app.Use(middleware.NewFallbackMiddleware(lookup, fallbackProxy.handler))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.Status(http.StatusBadGateway).JSON(fiber.Map{"error": "upstream"})
	})

	resp := postJSON(app, makeBody("gpt-4o"))
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "", fallbackProxy.seenModel, "must not call fallback when user is missing")
}

// TestFallbackMiddleware_Non502Status checks that 4xx errors do not trigger fallback.
func TestFallbackMiddleware_Non502Status(t *testing.T) {
	lookup := &mockFallbackLookup{fallbacks: map[string]*middleware.ModelConfig{
		"gpt-4o": {ID: "fb-1", Name: "gpt-4o-mini"},
	}}
	fallbackProxy := &proxyHandlerStub{statusCode: http.StatusOK}

	for _, code := range []int{400, 401, 403, 404, 422, 429} {
		app, _ := setupFallbackApp(lookup, code, fallbackProxy)
		resp := postJSON(app, makeBody("gpt-4o"))
		assert.Equal(t, code, resp.StatusCode, "status %d should pass through without fallback", code)
		assert.Equal(t, "", fallbackProxy.seenModel, "fallback must not run for status %d", code)
		fallbackProxy.seenModel = "" // reset
	}
}
