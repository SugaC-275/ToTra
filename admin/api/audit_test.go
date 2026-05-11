package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubAuditSvc struct{}

func (s *stubAuditSvc) GetAuditLog(_ context.Context, _ string, _ int) ([]*services.AuditEntry, error) {
	return []*services.AuditEntry{
		{
			ID: 1, TenantID: "tid", RecordType: "kpi_snapshot",
			RecordID: "snap-uuid-1", RecordHash: "aaaa", PrevHash: "genesis",
			ChainHash: "bbbb", CreatedAt: time.Now(),
		},
	}, nil
}

func (s *stubAuditSvc) VerifyChain(_ context.Context, _ string) (*services.VerifyResult, error) {
	return &services.VerifyResult{Valid: true}, nil
}

func setupAuditApp(svc *stubAuditSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterAuditRoutes(app, svc)
	return app
}

func TestAuditLog_List_OK(t *testing.T) {
	app := setupAuditApp(&stubAuditSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-log", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Len(t, result, 1)
	assert.Equal(t, "kpi_snapshot", result[0]["record_type"])
}

func TestAuditLog_List_LimitParam(t *testing.T) {
	app := setupAuditApp(&stubAuditSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-log?limit=10", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAuditLog_Verify_OK(t *testing.T) {
	app := setupAuditApp(&stubAuditSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-log/verify", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, true, result["valid"])
}

func TestAuditLog_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterAuditRoutes(app, &stubAuditSvc{})

	for _, path := range []string{"/api/admin/audit-log", "/api/admin/audit-log/verify"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, _ := app.Test(req)
		assert.Equal(t, 403, resp.StatusCode, "expected 403 for path %s", path)
	}
}
