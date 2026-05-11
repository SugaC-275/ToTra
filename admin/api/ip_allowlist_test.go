package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubAllowlistSvc struct {
	entries []*services.IPAllowlistEntry
	addErr  error
	delErr  error
}

func (s *stubAllowlistSvc) List(_ context.Context, _ string) ([]*services.IPAllowlistEntry, error) {
	return s.entries, nil
}
func (s *stubAllowlistSvc) Add(_ context.Context, _, cidr, label string) (*services.IPAllowlistEntry, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	return &services.IPAllowlistEntry{ID: "new-id", CIDR: cidr, Label: label, CreatedAt: "2026-05-11T00:00:00Z"}, nil
}
func (s *stubAllowlistSvc) Delete(_ context.Context, _, _ string) error {
	return s.delErr
}

func allowlistClaimsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	return app
}

func TestListIPAllowlist(t *testing.T) {
	app := allowlistClaimsApp()
	stub := &stubAllowlistSvc{
		entries: []*services.IPAllowlistEntry{
			{ID: "id-1", CIDR: "10.0.0.0/8", Label: "office", CreatedAt: "2026-05-11T00:00:00Z"},
		},
	}
	api.RegisterIPAllowlistRoutes(app, stub)

	req := httptest.NewRequest("GET", "/api/admin/ip-allowlist", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	entries := body["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestAddIPAllowlist(t *testing.T) {
	app := allowlistClaimsApp()
	stub := &stubAllowlistSvc{}
	api.RegisterIPAllowlistRoutes(app, stub)

	payload, _ := json.Marshal(map[string]string{"cidr": "192.168.0.0/16", "label": "hq"})
	req := httptest.NewRequest("POST", "/api/admin/ip-allowlist", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestAddIPAllowlist_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u2", TenantID: "t1", Role: "user"})
		return c.Next()
	})
	stub := &stubAllowlistSvc{}
	api.RegisterIPAllowlistRoutes(app, stub)

	payload, _ := json.Marshal(map[string]string{"cidr": "1.2.3.4/32", "label": ""})
	req := httptest.NewRequest("POST", "/api/admin/ip-allowlist", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestDeleteIPAllowlist(t *testing.T) {
	app := allowlistClaimsApp()
	stub := &stubAllowlistSvc{}
	api.RegisterIPAllowlistRoutes(app, stub)

	req := httptest.NewRequest("DELETE", "/api/admin/ip-allowlist/entry-id", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
