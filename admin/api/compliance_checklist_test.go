package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubChecklistSvc struct{}

func (s *stubChecklistSvc) GetChecklist(_ context.Context, _ string) ([]*services.ChecklistEntry, error) {
	return []*services.ChecklistEntry{{ItemKey: "data_governance", Status: "not_assessed"}}, nil
}
func (s *stubChecklistSvc) UpdateItem(_ context.Context, _, _, _, _ string) error { return nil }
func (s *stubChecklistSvc) ExportViolationsCSV(_ context.Context, _, _ string) (string, error) {
	return "occurred_at,user_email,pii_type,action,request_path\n", nil
}
func (s *stubChecklistSvc) GetSOC2Report(_ context.Context, _ string) (*services.SOC2Report, error) {
	return &services.SOC2Report{TenantID: "tid"}, nil
}

func setupChecklistApp(svc api.ChecklistServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterChecklistRoutes(app, svc)
	return app
}

func TestGetChecklist_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/checklist", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestUpdateChecklistItem_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	body := `{"status":"compliant","notes":"verified"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compliance/checklist/data_governance", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 204, resp.StatusCode)
}

func TestUpdateChecklistItem_MissingStatus(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compliance/checklist/data_governance", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestExportViolationsCSV_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/audit-export", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "text/csv", resp.Header.Get("Content-Type"))
}

func TestGetSOC2Report_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/soc2-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestChecklistRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterChecklistRoutes(app, &stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/checklist", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
