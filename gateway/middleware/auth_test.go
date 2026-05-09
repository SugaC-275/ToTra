package middleware_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type mockUserLookup struct {
	users map[string]*middleware.UserInfo
}

func (m *mockUserLookup) LookupByKeyHash(hash string) (*middleware.UserInfo, error) {
	u, ok := m.users[hash]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func hash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{
		hash("totra-emp-valid-key"): {UserID: "user-1", TenantID: "tenant-1", Role: "standard"},
	}}

	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error {
		u := c.Locals("user").(*middleware.UserInfo)
		return c.JSON(fiber.Map{"user_id": u.UserID})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer totra-emp-valid-key")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{}}
	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer bad-key")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{}}
	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}
