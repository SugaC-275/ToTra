package api_test

import (
	"bytes"
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

// ---- stubs ----

type stubDataRetentionSvc struct {
	months  int
	cleaned int64
	err     error
}

func (s *stubDataRetentionSvc) GetRetentionMonths(_ context.Context, _ string) (int, error) {
	return s.months, s.err
}
func (s *stubDataRetentionSvc) SetRetentionMonths(_ context.Context, _ string, months int) error {
	s.months = months
	return s.err
}
func (s *stubDataRetentionSvc) RunRetentionCleanup(_ context.Context, _ string) (int64, error) {
	return s.cleaned, s.err
}

type stubMetaStore struct {
	meta *api.RetentionCleanupMeta
	err  error
}

func (s *stubMetaStore) GetCleanupMeta(_ context.Context, _ string) (*api.RetentionCleanupMeta, error) {
	return s.meta, s.err
}
func (s *stubMetaStore) SaveCleanupMeta(_ context.Context, _ string, _ int64, _ time.Time) error {
	return s.err
}

// ---- helpers ----

func setupDataRetentionApp(svc api.RetentionSvcIface, meta api.RetentionCleanupMetaStore, role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: role})
		return c.Next()
	})
	api.RegisterDataRetentionRoutes(app, svc, meta)
	return app
}

// ---- GET /api/admin/data-retention/config ----

func TestGetRetentionConfig_OK(t *testing.T) {
	svc := &stubDataRetentionSvc{months: 12}
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rows := int64(500)
	meta := &stubMetaStore{meta: &api.RetentionCleanupMeta{
		LastCleanupAt:          &ts,
		LastCleanupRowsDeleted: &rows,
	}}
	app := setupDataRetentionApp(svc, meta, "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention/config", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(12), body["retention_months"])
	assert.NotNil(t, body["last_cleanup_at"])
	assert.Equal(t, float64(500), body["last_cleanup_rows_deleted"])
}

func TestGetRetentionConfig_NeverRun(t *testing.T) {
	svc := &stubDataRetentionSvc{months: 24}
	meta := &stubMetaStore{meta: &api.RetentionCleanupMeta{}}
	app := setupDataRetentionApp(svc, meta, "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention/config", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(24), body["retention_months"])
	assert.Nil(t, body["last_cleanup_at"])
	assert.Nil(t, body["last_cleanup_rows_deleted"])
}

func TestGetRetentionConfig_NonAdmin_Forbidden(t *testing.T) {
	app := setupDataRetentionApp(&stubDataRetentionSvc{months: 12}, &stubMetaStore{}, "standard")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention/config", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---- PUT /api/admin/data-retention/config ----

func TestPutRetentionConfig_OK(t *testing.T) {
	svc := &stubDataRetentionSvc{months: 24}
	app := setupDataRetentionApp(svc, &stubMetaStore{}, "admin")

	body, _ := json.Marshal(map[string]int{"months": 18})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 18, svc.months)
}

func TestPutRetentionConfig_RejectsZero(t *testing.T) {
	app := setupDataRetentionApp(&stubDataRetentionSvc{months: 24}, &stubMetaStore{}, "admin")

	body, _ := json.Marshal(map[string]int{"months": 0})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestPutRetentionConfig_RejectsOver84(t *testing.T) {
	app := setupDataRetentionApp(&stubDataRetentionSvc{months: 24}, &stubMetaStore{}, "admin")

	body, _ := json.Marshal(map[string]int{"months": 100})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestPutRetentionConfig_Accepts84(t *testing.T) {
	svc := &stubDataRetentionSvc{months: 24}
	app := setupDataRetentionApp(svc, &stubMetaStore{}, "admin")

	body, _ := json.Marshal(map[string]int{"months": 84})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 84, svc.months)
}

func TestPutRetentionConfig_NonAdmin_Forbidden(t *testing.T) {
	app := setupDataRetentionApp(&stubDataRetentionSvc{months: 24}, &stubMetaStore{}, "employee")
	body, _ := json.Marshal(map[string]int{"months": 12})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---- POST /api/admin/data-retention/cleanup ----

func TestPostRetentionCleanup_OK(t *testing.T) {
	svc := &stubDataRetentionSvc{cleaned: 1234}
	app := setupDataRetentionApp(svc, &stubMetaStore{}, "admin")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-retention/cleanup", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(1234), body["rows_deleted"])
	assert.NotEmpty(t, body["last_cleanup_at"])
}

func TestPostRetentionCleanup_NonAdmin_Forbidden(t *testing.T) {
	app := setupDataRetentionApp(&stubDataRetentionSvc{cleaned: 0}, &stubMetaStore{}, "standard")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-retention/cleanup", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
