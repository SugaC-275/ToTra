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
)

type stubRiskTrendSvc struct{}

func (s *stubRiskTrendSvc) GetRiskTrend(_ context.Context, tenantID string) (*services.RiskTrend, error) {
	return &services.RiskTrend{
		TenantID: tenantID,
		Points: []services.MonthlyRiskPoint{
			{Month: "2026-04", Violations: 10, Requests: 1000, RatePer1K: 10, RiskScore: 20},
			{Month: "2026-05", Violations: 5, Requests: 1000, RatePer1K: 5, RiskScore: 10},
		},
	}, nil
}

type stubDigestSvc struct{}

func (s *stubDigestSvc) GetDigest(_ context.Context, tenantID, _ string) (*services.ComplianceDigest, error) {
	return &services.ComplianceDigest{
		TenantID:        tenantID,
		TotalViolations: 15,
		TotalRequests:   2000,
		BlockRate:       7.5,
		ByPIIType:       []services.DigestPIITypeStat{{PIIType: "email", Count: 10}},
		TopViolators:    []services.UserViolationStat{{UserID: "u1", Count: 8}},
	}, nil
}

func makeReportsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterComplianceReportRoutes(app, &stubRiskTrendSvc{}, &stubDigestSvc{})
	return app
}

func TestRiskTrend_OK(t *testing.T) {
	resp, err := makeReportsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/risk-trend", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body services.RiskTrend
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.TenantID != "t1" {
		t.Fatalf("want t1, got %s", body.TenantID)
	}
	if len(body.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(body.Points))
	}
}

func TestComplianceDigest_OK(t *testing.T) {
	resp, err := makeReportsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/digest", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body services.ComplianceDigest
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.TotalViolations != 15 {
		t.Fatalf("want 15, got %d", body.TotalViolations)
	}
}

func TestComplianceReports_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterComplianceReportRoutes(app, &stubRiskTrendSvc{}, &stubDigestSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/risk-trend", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}
