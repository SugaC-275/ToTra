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

type stubProcurementSvc struct{}

func (s *stubProcurementSvc) GetProcurementReport(_ context.Context, tenantID string, months int) (*services.ProcurementReport, error) {
	return &services.ProcurementReport{
		TenantID:     tenantID,
		PeriodMonths: months,
		TotalUSD:     4800.0,
		ByProvider: []services.ProviderSpend{
			{Provider: "openai", TotalUSD: 3000, RequestCount: 10000, SharePercent: 62.5},
			{Provider: "anthropic", TotalUSD: 1800, RequestCount: 6000, SharePercent: 37.5},
		},
		TopModels:     []services.ModelSpend{{Model: "gpt-4o", Provider: "openai", TotalUSD: 2000, RequestCount: 5000}},
		PeakMonthUSD:  900,
		PeakMonth:     "2026-04",
		AvgMonthlyUSD: 800,
		ForecastedEOY: 9600,
		GeneratedAt:   "2026-05-20T00:00:00Z",
	}, nil
}

func makeProcurementApp(role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{TenantID: "t1", Role: role})
		return c.Next()
	})
	api.RegisterProcurementRoutes(app, &stubProcurementSvc{})
	return app
}

func TestGetProcurementReport_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/procurement-report", nil)
	resp, err := makeProcurementApp("admin").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.ProcurementReport
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	assert.Equal(t, 6, body.PeriodMonths)
	assert.InDelta(t, 4800.0, body.TotalUSD, 0.01)
	require.Len(t, body.ByProvider, 2)
	assert.Equal(t, "openai", body.ByProvider[0].Provider)
}

func TestGetProcurementReport_CustomMonths(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/procurement-report?months=12", nil)
	resp, err := makeProcurementApp("admin").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.ProcurementReport
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 12, body.PeriodMonths)
}

func TestGetProcurementReport_NonAdmin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/procurement-report", nil)
	resp, err := makeProcurementApp("user").Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
