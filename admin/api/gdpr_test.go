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

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubRetentionSvc struct {
	months  int
	err     error
	cleaned int64
}

func (s *stubRetentionSvc) GetRetentionMonths(_ context.Context, _ string) (int, error) {
	return s.months, s.err
}
func (s *stubRetentionSvc) SetRetentionMonths(_ context.Context, _ string, months int) error {
	s.months = months
	return s.err
}
func (s *stubRetentionSvc) RunRetentionCleanup(_ context.Context, _ string) (int64, error) {
	return s.cleaned, s.err
}

type stubDeletionSvc struct {
	req    *services.DeletionRequest
	reqs   []*services.DeletionRequest
	export *services.DataExport
	err    error
}

func (s *stubDeletionSvc) CreateRequest(_ context.Context, _, _ string) (*services.DeletionRequest, error) {
	return s.req, s.err
}
func (s *stubDeletionSvc) ListPending(_ context.Context, _ string) ([]*services.DeletionRequest, error) {
	return s.reqs, s.err
}
func (s *stubDeletionSvc) Approve(_ context.Context, _, _ string) error { return s.err }
func (s *stubDeletionSvc) Reject(_ context.Context, _, _ string) error  { return s.err }
func (s *stubDeletionSvc) ExportUserData(_ context.Context, _, _ string) (*services.DataExport, error) {
	return s.export, s.err
}

func setupGDPRApp(retSvc *stubRetentionSvc, delSvc *stubDeletionSvc, role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid-1", TenantID: "tid-1", Role: role})
		return c.Next()
	})
	api.RegisterGDPRRoutes(app, retSvc, delSvc)
	return app
}

func TestGetDataRetention_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 18}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(18), body["data_retention_months"])
}

func TestGetDataRetention_NonAdmin_Forbidden(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 24}, &stubDeletionSvc{}, "standard")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestPutDataRetention_Admin(t *testing.T) {
	svc := &stubRetentionSvc{months: 24}
	app := setupGDPRApp(svc, &stubDeletionSvc{}, "admin")
	body, _ := json.Marshal(map[string]int{"data_retention_months": 12})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 12, svc.months)
}

func TestPutDataRetention_InvalidMonths(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 24}, &stubDeletionSvc{}, "admin")
	body, _ := json.Marshal(map[string]int{"data_retention_months": 0})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestRunRetentionCleanup_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{cleaned: 42}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-retention/run", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(42), body["deleted_count"])
}

func TestListDeletionRequests_Admin(t *testing.T) {
	reqs := []*services.DeletionRequest{
		{ID: "r1", UserID: "u1", UserName: "Alice", Status: "pending", RequestedAt: "2026-05-01T00:00:00Z"},
	}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{reqs: reqs}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-deletion-requests", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	list := body["requests"].([]interface{})
	assert.Len(t, list, 1)
}

func TestApproveDeletionRequest_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRejectDeletionRequest_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/reject", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestApproveDeletionRequest_NonAdmin_Forbidden(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "standard")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestCreateDeletionRequest_Employee(t *testing.T) {
	dr := &services.DeletionRequest{ID: "r2", UserID: "uid-1", Status: "pending", RequestedAt: "2026-05-11T00:00:00Z"}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{req: dr}, "standard")
	req := httptest.NewRequest(http.MethodPost, "/api/me/data-deletion-request", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestExportMyData_Employee(t *testing.T) {
	export := &services.DataExport{
		ExportedAt: "2026-05-11T00:00:00Z",
		UserID:     "uid-1",
	}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{export: export}, "standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/data-export", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "uid-1", body["user_id"])
}
