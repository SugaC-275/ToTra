package api_test

import (
	"bytes"
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

// ---- stubs ----

type stubSIEMCfgSvc struct {
	configs []*services.SIEMConfig
	delErr  error
}

func (s *stubSIEMCfgSvc) Create(_ context.Context, _, _, _, _ string, _ []string) (*services.SIEMConfig, error) {
	return &services.SIEMConfig{
		ID: "new-id", Name: "Splunk", EndpointURL: "https://splunk.example.com",
		EventTypes: []string{"pii_violation"}, IsActive: true, CreatedAt: time.Now(),
	}, nil
}
func (s *stubSIEMCfgSvc) List(_ context.Context, _ string) ([]*services.SIEMConfig, error) {
	return s.configs, nil
}
func (s *stubSIEMCfgSvc) Delete(_ context.Context, _, _ string) error { return s.delErr }

type stubSIEMDelivSvc struct{ testErr error }

func (s *stubSIEMDelivSvc) SendTest(_ context.Context, _, _ string) error { return s.testErr }
func (s *stubSIEMDelivSvc) GetDeliveryLog(_ context.Context, _ string, _ int) ([]*services.DeliveryLogRow, error) {
	return []*services.DeliveryLogRow{
		{ID: 1, EventType: "pii_violation", Status: "delivered", Attempts: 0, CreatedAt: time.Now()},
	}, nil
}

type stubSIEMPullSvc struct{}

func (s *stubSIEMPullSvc) GetEvents(_ context.Context, _ string, _ time.Time, _ []string, _ int) (*services.SIEMEventsResult, error) {
	return &services.SIEMEventsResult{Events: []*services.SIEMEvent{}, NextSince: time.Now()}, nil
}

func setupSIEMApp(cfgSvc *stubSIEMCfgSvc, delivSvc *stubSIEMDelivSvc, pullSvc *stubSIEMPullSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterSIEMRoutes(app, cfgSvc, delivSvc, pullSvc)
	return app
}

// ---- tests ----

func TestListSIEMConfigs_Empty(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCreateSIEMConfig_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	body := `{"name":"Splunk","endpoint_url":"https://splunk.example.com/hec","api_key":"secret","event_types":["pii_violation"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestCreateSIEMConfig_MissingFields(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	body := `{"name":"Splunk"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestDeleteSIEMConfig_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/siem/configs/some-id", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSendSIEMTest_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs/some-id/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetSIEMEvents_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/events?limit=10", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetSIEMDeliveryLog_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/delivery-log", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSIEMRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "user"})
		return c.Next()
	})
	api.RegisterSIEMRoutes(app, &stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
