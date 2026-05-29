package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

func newSpendTagsApp() (*fiber.App, *[]string) {
	app := fiber.New()
	var captured []string
	app.Use(middleware.NewSpendTagsMiddleware())
	app.Post("/test", func(c *fiber.Ctx) error {
		if tags, ok := c.Locals("spend_tags").([]string); ok {
			captured = tags
		} else {
			captured = nil
		}
		return c.SendStatus(200)
	})
	return app, &captured
}

func TestSpendTagsMiddleware_XTagsHeader(t *testing.T) {
	app, captured := newSpendTagsApp()

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tags", "feature-x, team-backend, feature-x")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	// deduplication: feature-x appears once
	assert.Equal(t, []string{"feature-x", "team-backend"}, *captured)
}

func TestSpendTagsMiddleware_BodyMetadataTags(t *testing.T) {
	app, captured := newSpendTagsApp()

	body := `{"model":"gpt-4","metadata":{"tags":["cost-center-a","squad-infra"]}}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []string{"cost-center-a", "squad-infra"}, *captured)
}

func TestSpendTagsMiddleware_HeaderTakesPrecedenceOverBody(t *testing.T) {
	app, captured := newSpendTagsApp()

	// X-Tags header is present → body tags ignored
	body := `{"metadata":{"tags":["body-tag"]}}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tags", "header-tag")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []string{"header-tag"}, *captured)
}

func TestSpendTagsMiddleware_NoTags(t *testing.T) {
	app, captured := newSpendTagsApp()

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Nil(t, *captured)
}
