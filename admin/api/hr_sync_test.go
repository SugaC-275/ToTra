package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubHRSyncSvc struct {
	result *services.SyncResult
	err    error
}

func (s *stubHRSyncSvc) SyncFromCSV(_ context.Context, _ string, _ []services.HRRecord) (*services.SyncResult, error) {
	return s.result, s.err
}

func setupHRSyncApp(svc *stubHRSyncSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterHRSyncRoutes(app, svc)
	return app
}

func makeCSVUpload(t *testing.T, csvContent string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "hr.csv")
	part.Write([]byte(csvContent))
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestHRSync_OK(t *testing.T) {
	app := setupHRSyncApp(&stubHRSyncSvc{result: &services.SyncResult{Created: 2, Updated: 1}})
	csv := "email,name,role,department\nalice@acme.com,Alice,employee,Engineering\n"
	body, ct := makeCSVUpload(t, csv)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/hr/sync", body)
	req.Header.Set("Content-Type", ct)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, float64(2), result["created"])
}

func TestHRSync_MissingFile(t *testing.T) {
	app := setupHRSyncApp(&stubHRSyncSvc{result: &services.SyncResult{}})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/hr/sync", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestHRSync_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterHRSyncRoutes(app, &stubHRSyncSvc{})
	csv := "email,name,role,department\n"
	body, ct := makeCSVUpload(t, csv)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/hr/sync", body)
	req.Header.Set("Content-Type", ct)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
