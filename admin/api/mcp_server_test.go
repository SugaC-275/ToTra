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
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

// ---------------------------------------------------------------------------
// Stub service
// ---------------------------------------------------------------------------

type stubMCPServerSvc struct {
	servers  []*services.MCPServer
	toolLogs []*services.MCPToolCallLog
	total    int64
}

func (s *stubMCPServerSvc) List(_ context.Context, _ string) ([]*services.MCPServer, error) {
	return s.servers, nil
}

func (s *stubMCPServerSvc) Get(_ context.Context, _, id string) (*services.MCPServer, error) {
	for _, srv := range s.servers {
		if srv.ID == id {
			return srv, nil
		}
	}
	return nil, nil // not needed for current tests
}

func (s *stubMCPServerSvc) Create(_ context.Context, tenantID string, req services.CreateMCPServerRequest) (*services.MCPServer, error) {
	m := &services.MCPServer{
		ID:           "new-uuid",
		TenantID:     tenantID,
		Name:         req.Name,
		Description:  req.Description,
		URL:          req.URL,
		AuthType:     req.AuthType,
		Enabled:      true,
		MaxToolCalls: req.MaxToolCalls,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	s.servers = append(s.servers, m)
	return m, nil
}

func (s *stubMCPServerSvc) Update(_ context.Context, _, id string, req services.UpdateMCPServerRequest) (*services.MCPServer, error) {
	for _, srv := range s.servers {
		if srv.ID == id {
			if req.Description != nil {
				srv.Description = *req.Description
			}
			return srv, nil
		}
	}
	return nil, nil
}

func (s *stubMCPServerSvc) Delete(_ context.Context, _, id string) error {
	for i, srv := range s.servers {
		if srv.ID == id {
			s.servers = append(s.servers[:i], s.servers[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *stubMCPServerSvc) ListToolCalls(_ context.Context, _ string, _, _ int) ([]*services.MCPToolCallLog, int64, error) {
	return s.toolLogs, s.total, nil
}

// ---------------------------------------------------------------------------
// Test app factory
// ---------------------------------------------------------------------------

func setupMCPApp(svc api.MCPServerServiceInterface, role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: role})
		return c.Next()
	})
	api.RegisterMCPServerRoutes(app, svc)
	return app
}

func fixedServer() *services.MCPServer {
	return &services.MCPServer{
		ID:           "server-uuid-1",
		TenantID:     "tid",
		Name:         "my-server",
		Description:  "test server",
		URL:          "https://mcp.example.com",
		AuthType:     "none",
		Enabled:      true,
		MaxToolCalls: 10,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestMCPServer_List_OK(t *testing.T) {
	svc := &stubMCPServerSvc{servers: []*services.MCPServer{fixedServer()}}
	app := setupMCPApp(svc, "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(1), body["total"])
	servers := body["servers"].([]any)
	assert.Len(t, servers, 1)
	assert.Equal(t, "my-server", servers[0].(map[string]any)["name"])
}

func TestMCPServer_List_Empty(t *testing.T) {
	svc := &stubMCPServerSvc{servers: []*services.MCPServer{}}
	app := setupMCPApp(svc, "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	assert.Equal(t, float64(0), body["total"])
}

// ---------------------------------------------------------------------------
// Create — success
// ---------------------------------------------------------------------------

func TestMCPServer_Create_OK(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name":           "my-server",
		"url":            "https://mcp.example.com",
		"auth_type":      "none",
		"max_tool_calls": 20,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "my-server", result["name"])
	assert.Equal(t, float64(20), result["max_tool_calls"])
	// auth_token must NOT be present in response.
	_, hasToken := result["auth_token"]
	assert.False(t, hasToken)
}

func TestMCPServer_Create_BearerWithToken(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name":           "secure-server",
		"url":            "https://mcp.example.com",
		"auth_type":      "bearer",
		"auth_token":     "super-secret",
		"max_tool_calls": 5,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Create — validation failures (400)
// ---------------------------------------------------------------------------

func TestMCPServer_Create_400_MissingName(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{"url": "https://example.com", "auth_type": "none", "max_tool_calls": 10}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	assert.Contains(t, result["error"], "name")
}

func TestMCPServer_Create_400_InvalidNameChars(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name": "My Server!", // uppercase + special chars
		"url":  "https://example.com", "auth_type": "none", "max_tool_calls": 10,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestMCPServer_Create_400_BadURL(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name": "ok-name", "url": "ftp://bad-scheme.com",
		"auth_type": "none", "max_tool_calls": 10,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestMCPServer_Create_400_BearerMissingToken(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name": "ok-name", "url": "https://mcp.example.com",
		"auth_type": "bearer", "max_tool_calls": 10,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	assert.Contains(t, result["error"], "auth_token")
}

func TestMCPServer_Create_400_BadAuthType(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name": "ok-name", "url": "https://mcp.example.com",
		"auth_type": "apikey", "max_tool_calls": 10,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestMCPServer_Create_400_MaxToolCallsOutOfRange(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	// 0 is treated as "not provided" and defaults to 10 — only values explicitly
	// above 100 are rejected. The lower bound check applies to non-zero values.
	payload := map[string]any{
		"name": "ok-name", "url": "https://mcp.example.com",
		"auth_type": "none", "max_tool_calls": 101,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode, "expected 400 for max_tool_calls=101")
}

func TestMCPServer_Create_DefaultMaxToolCalls(t *testing.T) {
	// max_tool_calls omitted (JSON zero value = 0) → handler defaults to 10 → 201.
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "admin")

	payload := map[string]any{
		"name": "ok-name", "url": "https://mcp.example.com", "auth_type": "none",
		// max_tool_calls intentionally absent
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	assert.Equal(t, float64(10), result["max_tool_calls"])
}

// ---------------------------------------------------------------------------
// Create — 403 for non-admin
// ---------------------------------------------------------------------------

func TestMCPServer_Create_403_NonAdmin(t *testing.T) {
	svc := &stubMCPServerSvc{}
	app := setupMCPApp(svc, "employee")

	payload := map[string]any{
		"name": "ok-name", "url": "https://mcp.example.com",
		"auth_type": "none", "max_tool_calls": 10,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestMCPServer_Delete_OK(t *testing.T) {
	svc := &stubMCPServerSvc{servers: []*services.MCPServer{fixedServer()}}
	app := setupMCPApp(svc, "admin")

	req := httptest.NewRequest(http.MethodDelete, "/api/mcp-servers/server-uuid-1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	assert.Equal(t, "deleted", result["message"])
	assert.Len(t, svc.servers, 0)
}

func TestMCPServer_Delete_403_NonAdmin(t *testing.T) {
	svc := &stubMCPServerSvc{servers: []*services.MCPServer{fixedServer()}}
	app := setupMCPApp(svc, "employee")

	req := httptest.NewRequest(http.MethodDelete, "/api/mcp-servers/server-uuid-1", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Tool calls audit log
// ---------------------------------------------------------------------------

func TestMCPServer_ToolCalls_OK(t *testing.T) {
	logs := []*services.MCPToolCallLog{
		{ID: 1, ServerName: "my-server", ToolName: "list_files", StatusCode: 200, DurationMS: 10},
		{ID: 2, ServerName: "my-server", ToolName: "read_file", StatusCode: 200, DurationMS: 5, PIIDetected: true},
	}
	svc := &stubMCPServerSvc{toolLogs: logs, total: 2}
	app := setupMCPApp(svc, "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers/tool-calls?limit=50&offset=0", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, float64(2), result["total"])
	calls := result["tool_calls"].([]any)
	assert.Len(t, calls, 2)
	assert.Equal(t, true, calls[1].(map[string]any)["pii_detected"])
}
