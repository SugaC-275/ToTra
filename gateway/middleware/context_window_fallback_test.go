package middleware_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)

// echoModelApp builds a Fiber app that applies NewContextWindowFallbackMiddleware
// and then echoes the "model" field it received in the request body.
func echoModelApp() *fiber.App {
	app := fiber.New()
	app.Use(middleware.NewContextWindowFallbackMiddleware())
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("bad json")
		}
		return c.SendString(req.Model)
	})
	return app
}

// cwfPost sends a POST to the echo app and returns the response and body text.
func cwfPost(t *testing.T, app *fiber.App, body string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(raw)
}

// cwfPayload creates a JSON request body for the given model.
// padBytes controls body length, which drives EstimateTokensFromBody (~4 chars/token).
func cwfPayload(model string, padBytes int) string {
	padding := strings.Repeat("a", padBytes)
	return `{"model":"` + model + `","messages":[{"role":"user","content":"` + padding + `"}]}`
}

// TestContextWindowFallback_NoSwitch_BelowThreshold verifies that a request
// whose token estimate is well below 90 % of the model limit passes through
// without any model substitution.
func TestContextWindowFallback_NoSwitch_BelowThreshold(t *testing.T) {
	app := echoModelApp()

	// gpt-4 has an 8192-token limit. 90% threshold = ~7372 tokens ≈ 29488 chars.
	// 100 bytes → ~25 tokens → far below threshold → no switch.
	resp, got := cwfPost(t, app, cwfPayload("gpt-4", 100))

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gpt-4", got)
	assert.Empty(t, resp.Header.Get("X-Context-Fallback-From"))
	assert.Empty(t, resp.Header.Get("X-Context-Fallback-To"))
}

// TestContextWindowFallback_Switch_AboveThreshold verifies that when the token
// estimate exceeds 90 % of gpt-4's 8192-token limit the middleware patches the
// model to the next larger entry in the fallback chain (gpt-4o, 128k).
func TestContextWindowFallback_Switch_AboveThreshold(t *testing.T) {
	app := echoModelApp()

	// gpt-4 limit = 8192 tokens. 90 % threshold = 7372 tokens.
	// EstimateTokensFromBody = body / 4; need body > 29488 chars.
	// 40 000 bytes → ~10 000 tokens → exceeds threshold → switch to gpt-4o.
	resp, got := cwfPost(t, app, cwfPayload("gpt-4", 40_000))

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gpt-4o", got, "should upgrade to gpt-4o (128 k context)")
	assert.Equal(t, "gpt-4", resp.Header.Get("X-Context-Fallback-From"))
	assert.Equal(t, "gpt-4o", resp.Header.Get("X-Context-Fallback-To"))
}

// TestContextWindowFallback_UnknownModel_NoSwitch verifies that requests for
// models not in the context-limit registry are forwarded unchanged.
func TestContextWindowFallback_UnknownModel_NoSwitch(t *testing.T) {
	app := echoModelApp()

	resp, got := cwfPost(t, app, cwfPayload("some-future-model-xyz", 40_000))

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "some-future-model-xyz", got)
	assert.Empty(t, resp.Header.Get("X-Context-Fallback-From"))
}

// TestContextWindowFallback_AlreadyLargestModel_NoSwitch verifies that when
// the current model already sits at the top of the fallback chain no switch occurs.
// gemini-1.5-pro has a 1 M-token limit; 90 % = 900 k tokens.
// EstimateTokensFromBody is capped at 50 000, so the estimate can never exceed
// the threshold → the middleware correctly leaves the model unchanged.
func TestContextWindowFallback_AlreadyLargestModel_NoSwitch(t *testing.T) {
	app := echoModelApp()

	resp, got := cwfPost(t, app, cwfPayload("gemini-1.5-pro", 1_000))

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gemini-1.5-pro", got)
	assert.Empty(t, resp.Header.Get("X-Context-Fallback-From"))
}
