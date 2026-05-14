package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubPolicyRulesSvc struct{}

func (s *stubPolicyRulesSvc) List(_ context.Context, _ string) ([]*services.PolicyRule, error) {
	return []*services.PolicyRule{{ID: 1, Name: "test", Pattern: `\d+`, Action: "block", IsActive: true}}, nil
}
func (s *stubPolicyRulesSvc) Create(_ context.Context, _, name, pattern, action string) (*services.PolicyRule, error) {
	return &services.PolicyRule{ID: 2, Name: name, Pattern: pattern, Action: action, IsActive: true}, nil
}
func (s *stubPolicyRulesSvc) Update(_ context.Context, _ string, id int64, name, pattern, action string, isActive bool) (*services.PolicyRule, error) {
	return &services.PolicyRule{ID: id, Name: name, Pattern: pattern, Action: action, IsActive: isActive}, nil
}
func (s *stubPolicyRulesSvc) Delete(_ context.Context, _ string, _ int64) error { return nil }

func makePolicyApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterPolicyRulesRoutes(app, &stubPolicyRulesSvc{})
	return app
}

func TestPolicyRules_List(t *testing.T) {
	resp, err := makePolicyApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/policy-rules", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyRules_Create(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"name": "r1", "pattern": `\d+`, "action": "block"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/policy-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := makePolicyApp().Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestPolicyRules_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterPolicyRulesRoutes(app, &stubPolicyRulesSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/policy-rules", nil))
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
