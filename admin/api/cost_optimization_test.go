package api_test

import (
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

type stubOptSvc struct{}

func (s *stubOptSvc) GetSuggestions(_ context.Context, tenantID string) (*services.OptimizationReport, error) {
	return &services.OptimizationReport{
		TenantID:  tenantID,
		YearMonth: "2026-05",
		Suggestions: []*services.OptSuggestion{
			{Category: "scheduling", Title: "Restrict off-hours usage", Priority: "high", EstimatedSavingsUSD: 120},
		},
		GeneratedAt: "2026-05-19T00:00:00Z",
	}, nil
}

func makeOptApp(role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{TenantID: "t1", Role: role})
		return c.Next()
	})
	api.RegisterCostOptimizationRoutes(app, &stubOptSvc{})
	return app
}

func TestGetOptimizationSuggestions_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/optimization-suggestions", nil)
	resp, err := makeOptApp("admin").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.OptimizationReport
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	require.Len(t, body.Suggestions, 1)
	assert.Equal(t, "high", body.Suggestions[0].Priority)
}

func TestGetOptimizationSuggestions_NonAdmin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/optimization-suggestions", nil)
	resp, err := makeOptApp("user").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
