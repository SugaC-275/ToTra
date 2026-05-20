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

type stubBudgetPlannerSvc struct{}

func (s *stubBudgetPlannerSvc) GetBudgetPlan(_ context.Context, tenantID string, planYear int) (*services.BudgetPlan, error) {
	return &services.BudgetPlan{
		TenantID:       tenantID,
		PlanYear:       planYear,
		TotalAnnualUSD: 12000,
		GrowthRateUsed: 5.0,
		Quarterly: []services.QuarterlyBudget{
			{Quarter: "2027-Q1", USD: 2800},
			{Quarter: "2027-Q2", USD: 2950},
			{Quarter: "2027-Q3", USD: 3100},
			{Quarter: "2027-Q4", USD: 3150},
		},
		ByDepartment: []services.DeptMonthlyBudget{
			{Department: "Engineering", MonthlyUSD: 700, AnnualUSD: 8400, GrowthRate: 5.0},
		},
		GeneratedAt: "2026-05-20T00:00:00Z",
	}, nil
}

func makeBudgetPlanApp(role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{TenantID: "t1", Role: role})
		return c.Next()
	})
	api.RegisterBudgetPlannerRoutes(app, &stubBudgetPlannerSvc{})
	return app
}

func TestGetBudgetPlan_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-plan", nil)
	resp, err := makeBudgetPlanApp("admin").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.BudgetPlan
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	assert.InDelta(t, 12000.0, body.TotalAnnualUSD, 0.01)
	require.Len(t, body.Quarterly, 4)
}

func TestGetBudgetPlan_CustomYear(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-plan?year=2028", nil)
	resp, err := makeBudgetPlanApp("admin").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.BudgetPlan
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 2028, body.PlanYear)
}

func TestGetBudgetPlan_NonAdmin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-plan", nil)
	resp, err := makeBudgetPlanApp("user").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
