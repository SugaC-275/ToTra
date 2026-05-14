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

type stubCostSettingsSvc struct{}

func (s *stubCostSettingsSvc) Get(_ context.Context, tenantID string) (*services.CostSettings, error) {
	budget := 500.0
	return &services.CostSettings{TenantID: tenantID, MonthlyBudgetUSD: &budget, WorkStartHour: 9, WorkEndHour: 18}, nil
}
func (s *stubCostSettingsSvc) Upsert(_ context.Context, tenantID string, budget *float64, start, end int) (*services.CostSettings, error) {
	return &services.CostSettings{TenantID: tenantID, MonthlyBudgetUSD: budget, WorkStartHour: start, WorkEndHour: end}, nil
}

type stubBudgetStatusSvc struct{}

func (s *stubBudgetStatusSvc) GetBudgetStatus(_ context.Context, tenantID string) (*services.BudgetStatus, error) {
	budget := 500.0
	return &services.BudgetStatus{TenantID: tenantID, SpentUSD: 123.45, MonthlyBudgetUSD: &budget, UsedPercent: 24.69}, nil
}

type stubOffHoursSvc struct{}

func (s *stubOffHoursSvc) GetOffHoursReport(_ context.Context, tenantID, _ string) (*services.OffHoursReport, error) {
	return &services.OffHoursReport{TenantID: tenantID, OffHoursPercent: 12.5}, nil
}

func makeCostSettingsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostSettingsRoutes(app, &stubCostSettingsSvc{}, &stubBudgetStatusSvc{}, &stubOffHoursSvc{})
	return app
}

func TestCostSettings_Get(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/settings", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCostSettings_Upsert(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"monthly_budget_usd": 1000.0, "work_start_hour": 8, "work_end_hour": 17})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/cost/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := makeCostSettingsApp().Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestBudgetStatus(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-status", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.BudgetStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.InDelta(t, 123.45, body.SpentUSD, 0.01)
}

func TestOffHoursReport(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/off-hours", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCostSettings_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterCostSettingsRoutes(app, &stubCostSettingsSvc{}, &stubBudgetStatusSvc{}, &stubOffHoursSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/settings", nil))
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
