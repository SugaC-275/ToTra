package api_test

import (
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

type mockAgentService struct{}

func (m *mockAgentService) GetAgentSessions(_ context.Context, _, _ string) ([]*services.AgentSession, error) {
	return []*services.AgentSession{
		{
			ID: "sess-1", TenantID: "tenant-1", UserID: "user-1", UserName: "Alice",
			ConversationID: "conv-1", LoopCount: 8, ToolCallCount: 5, IsDeadLoop: false,
			LastSeenAt: time.Now().Format(time.RFC3339), CreatedAt: time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (m *mockAgentService) GetMyAgentSessions(_ context.Context, _, _ string) ([]*services.AgentSession, error) {
	return []*services.AgentSession{
		{
			ID: "sess-2", UserID: "user-2", UserName: "Bob",
			ConversationID: "conv-2", LoopCount: 22, ToolCallCount: 18, IsDeadLoop: true,
			LastSeenAt: time.Now().Format(time.RFC3339), CreatedAt: time.Now().Format(time.RFC3339),
		},
	}, nil
}

func setupAgentAPIApp(role string) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "user-1", TenantID: "tenant-1", Role: role}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterAgentRoutes(app, &mockAgentService{})
	return app
}

func TestGetAdminAgentSessions_OK(t *testing.T) {
	app := setupAgentAPIApp("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetAdminAgentSessions_ForbiddenForEmployee(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestGetAdminAgentSessions_MissingMonth(t *testing.T) {
	app := setupAgentAPIApp("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetMyAgentSessions_OK(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetMyAgentSessions_MissingMonth(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/agent-sessions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}
