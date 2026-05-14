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

type stubAnomalySvc struct{}

func (s *stubAnomalySvc) GetAnomalies(_ context.Context, _ string) ([]*services.AnomalyUser, error) {
	return []*services.AnomalyUser{
		{UserID: "u1", UserName: "Alice", Severity: "high", RecentCount: 10},
	}, nil
}

type stubBenchmarkSvc struct{}

func (s *stubBenchmarkSvc) GetComplianceBenchmark(_ context.Context, _ string) (*services.ComplianceBenchmark, error) {
	return &services.ComplianceBenchmark{YourRate: 5.0, P50: 3.0, TenantCount: 10}, nil
}

func setupAdvancedComplianceApp(aSvc api.AnomalyServiceIface, bSvc api.ComplianceBenchmarkServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterComplianceAdvancedRoutes(app, aSvc, bSvc)
	return app
}

func TestGetAnomalies_OK(t *testing.T) {
	app := setupAdvancedComplianceApp(&stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/anomalies", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetComplianceBenchmark_OK(t *testing.T) {
	app := setupAdvancedComplianceApp(&stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/benchmark", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestComplianceAdvanced_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterComplianceAdvancedRoutes(app, &stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/anomalies", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
