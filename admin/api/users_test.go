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
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockUserService struct {
	users []*services.User
}

func (m *mockUserService) List(ctx context.Context, tenantID string) ([]*services.User, error) {
	return m.users, nil
}

func (m *mockUserService) Create(ctx context.Context, tenantID string, req services.CreateUserRequest) (*services.User, error) {
	return &services.User{ID: "new-id", Name: req.Name, Email: req.Email, Role: req.Role, APIKey: "totra-emp-new-key"}, nil
}

func setupTestApp(svc services.UserServiceInterface) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	})
	api.RegisterUserRoutes(app, svc)
	return app
}

func TestListUsers(t *testing.T) {
	svc := &mockUserService{users: []*services.User{
		{ID: "u1", Name: "Alice", Email: "alice@corp.com", Role: "standard"},
	}}
	app := setupTestApp(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(1), body["total"])
}

func TestCreateUser(t *testing.T) {
	svc := &mockUserService{}
	app := setupTestApp(svc)

	payload := map[string]string{"name": "Bob", "email": "bob@corp.com", "role": "standard"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, 201, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.NotEmpty(t, body["api_key"])
}
