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

type mockQuotaService struct{}

func (m *mockQuotaService) RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error {
	return nil
}
func (m *mockQuotaService) ListPending(ctx context.Context, tenantID string) ([]*services.QuotaRequest, error) {
	return []*services.QuotaRequest{{ID: "req-1", UserID: "u1", NewQuota: 100000, Reason: "Need more", Status: "pending"}}, nil
}
func (m *mockQuotaService) Approve(ctx context.Context, tenantID, requestID, reviewerID string) error {
	return nil
}
func (m *mockQuotaService) Reject(ctx context.Context, tenantID, requestID, reviewerID string) error {
	return nil
}
func (m *mockQuotaService) GetMyStatus(ctx context.Context, tenantID, userID, month string) (*services.QuotaStatus, error) {
	return &services.QuotaStatus{UsedSCU: 5000, QuotaSCU: 10000}, nil
}

func setupQuotaApp() *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterQuotaRoutes(app, &mockQuotaService{})
	return app
}

func TestListPendingQuotaRequests(t *testing.T) {
	app := setupQuotaApp()
	req := httptest.NewRequest(http.MethodGet, "/api/quota/requests", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestApproveQuotaRequest(t *testing.T) {
	app := setupQuotaApp()
	req := httptest.NewRequest(http.MethodPost, "/api/quota/requests/req-1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
