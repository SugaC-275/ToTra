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
	"github.com/yourorg/totra/gateway/middleware"
)

func newModelAliasApp(user *middleware.UserInfo) (*fiber.App, *string) {
	app := fiber.New()
	var capturedModel string
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Use(middleware.NewModelAliasMiddleware())
	app.Post("/test", func(c *fiber.Ctx) error {
		var req struct{ Model string `json:"model"` }
		_ = json.Unmarshal(c.Body(), &req)
		capturedModel = req.Model
		return c.SendStatus(200)
	})
	return app, &capturedModel
}

func TestModelAliasMiddleware_RewritesModel(t *testing.T) {
	user := &middleware.UserInfo{
		UserID:       "u1",
		TenantID:     "t1",
		ModelAliases: map[string]string{"gpt-4": "gpt-4o-mini"},
	}
	app, captured := newModelAliasApp(user)

	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4o-mini", *captured)
	assert.Equal(t, "gpt-4", resp.Header.Get("X-Model-Alias-From"))
	assert.Equal(t, "gpt-4o-mini", resp.Header.Get("X-Model-Alias-To"))
}

func TestModelAliasMiddleware_PassthroughWhenNoAlias(t *testing.T) {
	user := &middleware.UserInfo{
		UserID:       "u1",
		TenantID:     "t1",
		ModelAliases: map[string]string{"claude-3": "claude-3-haiku"},
	}
	app, captured := newModelAliasApp(user)

	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4", *captured)
	assert.Empty(t, resp.Header.Get("X-Model-Alias-From"))
}

func TestModelAliasMiddleware_PassthroughWhenNoUser(t *testing.T) {
	app := fiber.New()
	var capturedModel string
	app.Use(middleware.NewModelAliasMiddleware())
	app.Post("/test", func(c *fiber.Ctx) error {
		var req struct{ Model string `json:"model"` }
		_ = json.Unmarshal(c.Body(), &req)
		capturedModel = req.Model
		return c.SendStatus(200)
	})

	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4", capturedModel)
}

func TestModelAliasMiddleware_PassthroughWhenNoAliasMap(t *testing.T) {
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1"}
	app, captured := newModelAliasApp(user)

	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "gpt-4", *captured)
	// ensure resp body was not consumed
	_, _ = io.ReadAll(resp.Body)
}
