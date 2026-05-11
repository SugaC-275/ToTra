package middleware_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type mockAgentLoopIncr struct {
	count    int64
	exceeded bool
}

func (m *mockAgentLoopIncr) IncrLoop(_ context.Context, _ string) (int64, bool, error) {
	return m.count, m.exceeded, nil
}

func setupAgentApp(loopIncr middleware.AgentLoopIncr) *fiber.App {
	app := fiber.New()
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1", Role: "standard"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Use(middleware.NewAgentMiddleware(loopIncr))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func agentBody() *bytes.Reader {
	body := `{"model":"gpt-4","tools":[{"type":"function","function":{"name":"search"}}]}`
	return bytes.NewReader([]byte(body))
}

func nonAgentBody() *bytes.Reader {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	return bytes.NewReader([]byte(body))
}

func TestAgentMiddleware_NonAgent_Passes(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 0, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nonAgentBody())
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAgentMiddleware_AgentBelowLimit_Passes(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 5, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", agentBody())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "11111111-1111-1111-1111-111111111111")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAgentMiddleware_AgentExceedsLimit_Returns429(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 21, exceeded: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", agentBody())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "22222222-2222-2222-2222-222222222222")
	resp, _ := app.Test(req)
	assert.Equal(t, 429, resp.StatusCode)
}

func TestAgentMiddleware_ToolCallsInMessages_DetectedAsAgent(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}]}`
	app := setupAgentApp(&mockAgentLoopIncr{count: 1, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "33333333-3333-3333-3333-333333333333")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
