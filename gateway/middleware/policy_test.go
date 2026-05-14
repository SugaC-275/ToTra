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

type stubPolicyStore struct {
	rules []*middleware.PolicyRule
}

func (s *stubPolicyStore) GetRules(_ context.Context, _ string) ([]*middleware.PolicyRule, error) {
	return s.rules, nil
}

func setupPolicyApp(rules []*middleware.PolicyRule) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPolicyMiddleware(&stubPolicyStore{rules: rules}))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	return app
}

func TestPolicyMiddleware_NoRules(t *testing.T) {
	app := setupPolicyApp(nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyMiddleware_BlockRule_Matches(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"SSN: 123-45-6789"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPolicyMiddleware_BlockRule_NoMatch(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hello world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyMiddleware_LogRule_Passes(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "log-keyword", Pattern: `secret`, Action: "log"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"top secret project"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
