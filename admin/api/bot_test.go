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

type stubBotSvc struct {
	configs []services.BotConfig
	addErr  error
	delErr  error
	testErr error
}

func (s *stubBotSvc) List(_ context.Context, _ string) ([]services.BotConfig, error) {
	return s.configs, nil
}
func (s *stubBotSvc) Add(_ context.Context, _, _, _, _ string) (*services.BotConfig, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	c := &services.BotConfig{ID: "new-id", Platform: "feishu", Label: "test", Enabled: true, CreatedAt: time.Now()}
	return c, nil
}
func (s *stubBotSvc) Delete(_ context.Context, _, _ string) error         { return s.delErr }
func (s *stubBotSvc) SendTestMessage(_ context.Context, _, _ string) error { return s.testErr }

func setupBotApp(svc *stubBotSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterBotRoutes(app, svc)
	return app
}

func TestListBotConfigs_Empty(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/bot-configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	configs := result["configs"].([]interface{})
	assert.Len(t, configs, 0)
}

func TestAddBotConfig_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	body := `{"platform":"feishu","webhook_url":"https://open.feishu.cn/hook/abc","label":"main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestAddBotConfig_MissingFields(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	body := `{"platform":"feishu"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestDeleteBotConfig_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/bot-configs/some-id", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSendTestMessage_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs/some-id/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestBotRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterBotRoutes(app, &stubBotSvc{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bot-configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
