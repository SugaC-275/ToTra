package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBudgetSvc struct{}

func (s *stubBudgetSvc) GetBudgetForecast(ctx context.Context, tenantID string) (*services.EOYBudgetForecast, error) {
	return &services.EOYBudgetForecast{
		TenantID:      tenantID,
		ForecastedEOY: 1234.56,
		History:       []services.MonthlySpend{{Month: "2026-05", TotalUSD: 100}},
		GeneratedAt:   "2026-05-14T00:00:00Z",
	}, nil
}

type stubCostBenchmarkSvc struct{}

func (s *stubCostBenchmarkSvc) GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.CostBenchmark, error) {
	return &services.CostBenchmark{
		YearMonth:        "2026-05",
		TenantPerUserUSD: 50.0,
		P25:              20.0,
		P50:              40.0,
		P75:              80.0,
		TenantCount:      10,
		PercentileRank:   60.0,
		InsufficientData: false,
	}, nil
}

func (s *stubCostBenchmarkSvc) GetModelROI(ctx context.Context) ([]services.ModelROI, error) {
	return []services.ModelROI{
		{Model: "gpt-4o-mini", TotalRequests: 500, USDPer1KRequests: 0.15},
	}, nil
}

func makeCostAdvancedApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostAdvancedRoutes(app, &stubBudgetSvc{}, &stubCostBenchmarkSvc{})
	return app
}

func TestCostBudgetForecast(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-forecast", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.EOYBudgetForecast
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	assert.InDelta(t, 1234.56, body.ForecastedEOY, 0.01)
}

func TestCostBenchmark(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/benchmark", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.CostBenchmark
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "2026-05", body.YearMonth)
	assert.False(t, body.InsufficientData)
}

func TestCostModelROI(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/model-roi", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body []services.ModelROI
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body, 1)
	assert.Equal(t, "gpt-4o-mini", body[0].Model)
}

func TestCostBudgetForecast_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "user"})
		return c.Next()
	})
	api.RegisterCostAdvancedRoutes(app, &stubBudgetSvc{}, &stubCostBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-forecast", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
