package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubTopSpendersSvc struct{}

func (s *stubTopSpendersSvc) GetTopSpenders(_ context.Context, tenantID, yearMonth string, limit int) (*services.TopSpendersReport, error) {
	return &services.TopSpendersReport{
		TenantID:  tenantID,
		YearMonth: "2026-05",
		Entries: []services.SpenderEntry{
			{UserID: "u1", Email: "a@x.com", CurrentUSD: 50.0, PreviousUSD: 40.0, ChangeUSD: 10.0},
		},
		GeneratedAt: "2026-05-14T00:00:00Z",
	}, nil
}

type stubBudgetAlertSvc struct{ called bool }

func (s *stubBudgetAlertSvc) CheckAndNotify(_ context.Context, tenantID string, bot services.BotBroadcaster) error {
	s.called = true
	return nil
}

type stubBotBroadcaster struct{}

func (s *stubBotBroadcaster) BroadcastAlert(_ context.Context, tenantID, message string) error {
	return nil
}

func makeCostReportsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostReportsRoutes(app, &stubTopSpendersSvc{}, &stubBudgetAlertSvc{}, &stubBotBroadcaster{})
	return app
}

func TestTopSpenders_OK(t *testing.T) {
	resp, err := makeCostReportsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/top-spenders", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body services.TopSpendersReport
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(body.Entries))
	}
}

func TestTopSpenders_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{TenantID: "t1", Role: "user"})
		return c.Next()
	})
	api.RegisterCostReportsRoutes(app, &stubTopSpendersSvc{}, &stubBudgetAlertSvc{}, &stubBotBroadcaster{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/top-spenders", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestBudgetAlertCheck_OK(t *testing.T) {
	resp, err := makeCostReportsApp().Test(httptest.NewRequest(http.MethodPost, "/api/admin/cost/budget-alert/check", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}
