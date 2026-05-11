package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubInsightSvc struct {
	insight string
	err     error
}

func (s *stubInsightSvc) GenerateInsight(_ context.Context, _, _ string) (string, error) {
	return s.insight, s.err
}

func setupInsightApp(svc *stubInsightSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterAIInsightsRoutes(app, svc)
	return app
}

func TestGetKPIInsights_OK(t *testing.T) {
	app := setupInsightApp(&stubInsightSvc{insight: "Great work this month!"})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "Great work this month!", result["insight"])
	assert.Equal(t, "2026-05", result["month"])
}

func TestGetKPIInsights_DefaultMonth(t *testing.T) {
	app := setupInsightApp(&stubInsightSvc{insight: "AI insight text"})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result["month"])
}

func TestGetKPIInsights_Unauthenticated(t *testing.T) {
	app := fiber.New()
	api.RegisterAIInsightsRoutes(app, &stubInsightSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}
