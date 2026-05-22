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

// mockRBACService implements services.RBACServiceIface for tests.
type mockRBACService struct {
	roles     []services.UserRole
	assignErr error
	revokeErr error
	hasRole   bool
	hasErr    error
}

func (m *mockRBACService) GetUserRoles(_ context.Context, _, _ string) ([]services.UserRole, error) {
	return m.roles, nil
}

func (m *mockRBACService) AssignRole(_ context.Context, _, _ string, _ services.Role, _, _ string) error {
	return m.assignErr
}

func (m *mockRBACService) RevokeRole(_ context.Context, _, _ string, _ services.Role, _ string) error {
	return m.revokeErr
}

func (m *mockRBACService) HasRole(_ context.Context, _, _ string, _ services.Role) (bool, error) {
	return m.hasRole, m.hasErr
}

func setupRBACApp(svc services.RBACServiceIface, claims *services.Claims) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	})
	api.RegisterRBACRoutes(app, svc)
	return app
}

func adminClaims() *services.Claims {
	return &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
}

func TestListUserRoles_ReturnsRoles(t *testing.T) {
	svc := &mockRBACService{
		roles: []services.UserRole{
			{ID: "r1", UserID: "user-1", Role: services.RoleAuditor, Department: "", CreatedAt: time.Now()},
		},
	}
	app := setupRBACApp(svc, adminClaims())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/user-1/roles", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "user-1", body["user_id"])
	roles := body["roles"].([]interface{})
	assert.Len(t, roles, 1)
}

func TestListUserRoles_Empty(t *testing.T) {
	svc := &mockRBACService{roles: []services.UserRole{}}
	app := setupRBACApp(svc, adminClaims())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/user-99/roles", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAssignRole_Valid(t *testing.T) {
	svc := &mockRBACService{}
	app := setupRBACApp(svc, adminClaims())

	payload := map[string]string{"role": "auditor", "department": ""}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/user-1/roles", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "assigned", body["status"])
}

func TestAssignRole_InvalidRole(t *testing.T) {
	svc := &mockRBACService{}
	app := setupRBACApp(svc, adminClaims())

	payload := map[string]string{"role": "superuser"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/user-1/roles", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestAssignRole_MissingRole(t *testing.T) {
	svc := &mockRBACService{}
	app := setupRBACApp(svc, adminClaims())

	payload := map[string]string{}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/user-1/roles", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestRevokeRole_Found(t *testing.T) {
	svc := &mockRBACService{
		roles: []services.UserRole{
			{ID: "r1", UserID: "user-1", Role: services.RoleAuditor, Department: "", CreatedAt: time.Now()},
		},
	}
	app := setupRBACApp(svc, adminClaims())

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/user-1/roles/r1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "revoked", body["status"])
}

func TestRevokeRole_NotFound(t *testing.T) {
	svc := &mockRBACService{roles: []services.UserRole{}}
	app := setupRBACApp(svc, adminClaims())

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/user-1/roles/nonexistent", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestRequireAnyRole_AdminJWT_Passes(t *testing.T) {
	svc := &mockRBACService{hasRole: false}
	app := fiber.New()
	claims := &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	app.Get("/protected", api.RequireAnyRole(svc, "auditor"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRequireAnyRole_JWTClaimMatch_Passes(t *testing.T) {
	svc := &mockRBACService{hasRole: false}
	app := fiber.New()
	claims := &services.Claims{UserID: "u1", TenantID: "t1", Role: "auditor"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	app.Get("/protected", api.RequireAnyRole(svc, "auditor", "compliance_officer"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRequireAnyRole_SupplementalRoleMatch_Passes(t *testing.T) {
	svc := &mockRBACService{hasRole: true}
	app := fiber.New()
	claims := &services.Claims{UserID: "u1", TenantID: "t1", Role: "user"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	app.Get("/protected", api.RequireAnyRole(svc, "auditor"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRequireAnyRole_NoMatch_Forbidden(t *testing.T) {
	svc := &mockRBACService{hasRole: false}
	app := fiber.New()
	claims := &services.Claims{UserID: "u1", TenantID: "t1", Role: "user"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	app.Get("/protected", api.RequireAnyRole(svc, "auditor", "compliance_officer"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
