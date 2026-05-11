package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockUsageService struct{}

func (m *mockUsageService) GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*services.UserMonthlySummary, error) {
	return []*services.UserMonthlySummary{
		{UserID: "u1", UserName: "Alice", TotalSCU: 5000, TotalUSD: 12.50, RequestCount: 120},
	}, nil
}

func (m *mockUsageService) GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*services.AdoptionStats, error) {
	return &services.AdoptionStats{TotalUsers: 10, ActiveUsers: 7, AdoptionRate: 0.70}, nil
}

func (m *mockUsageService) GetDepartmentSummary(ctx context.Context, tenantID, yearMonth string) ([]*services.DeptSummary, error) {
	return []*services.DeptSummary{}, nil
}

func (m *mockUsageService) GetBudgetForecast(ctx context.Context, tenantID, yearMonth string) (*services.BudgetForecast, error) {
	return &services.BudgetForecast{}, nil
}

func (m *mockUsageService) GetInactiveUsers(ctx context.Context, tenantID, yearMonth string, maxDays int) ([]*services.InactiveUser, error) {
	return []*services.InactiveUser{}, nil
}

func setupUsageApp() *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterUsageRoutes(app, &mockUsageService{})
	return app
}

func TestGetMonthlySummary(t *testing.T) {
	app := setupUsageApp()
	req := httptest.NewRequest(http.MethodGet, "/api/usage/summary?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetAdoptionRate(t *testing.T) {
	app := setupUsageApp()
	req := httptest.NewRequest(http.MethodGet, "/api/usage/adoption?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
