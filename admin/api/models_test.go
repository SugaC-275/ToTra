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

type mockModelService struct{}

func (m *mockModelService) List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error) {
	return []*services.ModelConfig{
		{ID: "m1", Name: "gpt-4o", Provider: "openai", SCURate: 2.0, IsActive: true},
	}, nil
}

func (m *mockModelService) Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error) {
	return &services.ModelConfig{ID: "m2", Name: req.Name, Provider: req.Provider, SCURate: req.SCURate}, nil
}

func (m *mockModelService) UpdatePricing(_ context.Context, _, _ string, input, output float64) (*services.ModelConfig, error) {
	p := input
	q := output
	return &services.ModelConfig{ID: "m1", Name: "gpt-4o", PricePerMInput: &p, PricePerMOutput: &q}, nil
}

func (m *mockModelService) UpdateCacheSettings(_ context.Context, _, _ string, cacheDisabled bool) (*services.ModelConfig, error) {
	return &services.ModelConfig{ID: "m1", Name: "gpt-4o", CacheDisabled: cacheDisabled}, nil
}

func setupModelApp(svc api.ModelServiceInterface) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterModelRoutes(app, svc)
	return app
}

func TestListModels(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestUpdateModelPricing_OK(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	body := `{"price_per_m_input":5.00,"price_per_m_output":15.00}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/models/m1/pricing", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestUpdateModelPricing_MissingFields(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	body := `{"price_per_m_input":5.00}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/models/m1/pricing", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestCreateModel(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	payload := map[string]interface{}{"name": "claude-3-5-sonnet", "provider": "anthropic", "base_url": "https://api.anthropic.com", "api_key": "sk-ant-test", "scu_rate": 1.0}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/models", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}
