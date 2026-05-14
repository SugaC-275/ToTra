package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubSavingsSvc struct{}

func (s *stubSavingsSvc) GetMonthlySavings(_ context.Context, _, _ string) (*services.MonthlySavingsReport, error) {
	return &services.MonthlySavingsReport{
		YearMonth:          "2026-05",
		RoutingEventCount:  10,
		RoutingEventModels: []services.RoutedModelStat{},
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func setupSavingsApp(svc api.CostSavingsServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostSavingsRoutes(app, svc)
	return app
}

func TestGetCostSavingsReport_OK(t *testing.T) {
	app := setupSavingsApp(&stubSavingsSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/savings-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetCostSavingsReport_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterCostSavingsRoutes(app, &stubSavingsSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/savings-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
