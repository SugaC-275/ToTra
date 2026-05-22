package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yourorg/totra/admin/services"
)

// AlertPushServiceIface is the interface satisfied by services.AlertPushService.
type AlertPushServiceIface interface {
	GetConfigs(ctx context.Context, tenantID string) ([]services.AlertDeliveryConfig, error)
	CreateConfig(ctx context.Context, tenantID string, cfg services.AlertDeliveryConfig) error
	DeleteConfig(ctx context.Context, tenantID, configID string) error
	Deliver(ctx context.Context, event services.AlertEvent) error
}

// RegisterAlertConfigRoutes registers alert delivery config endpoints onto app.
func RegisterAlertConfigRoutes(app fiber.Router, svc AlertPushServiceIface) {
	g := app.Group("/api/admin/alert-configs")
	g.Get("/", listAlertConfigs(svc))
	g.Post("/", createAlertConfig(svc))
	g.Delete("/:id", deleteAlertConfig(svc))
	g.Post("/test", sendTestAlert(svc))
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func listAlertConfigs(svc AlertPushServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		configs, err := svc.GetConfigs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if configs == nil {
			configs = []services.AlertDeliveryConfig{}
		}
		return c.JSON(fiber.Map{"total": len(configs), "configs": configs})
	}
}

func createAlertConfig(svc AlertPushServiceIface) fiber.Handler {
	type request struct {
		Channel     string   `json:"channel"`
		Destination string   `json:"destination"`
		EventTypes  []string `json:"event_types"`
		Enabled     *bool    `json:"enabled"`
	}
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		var req request
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Channel == "" || req.Destination == "" || len(req.EventTypes) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "channel, destination, event_types are required"})
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		cfg := services.AlertDeliveryConfig{
			Channel:     req.Channel,
			Destination: req.Destination,
			EventTypes:  req.EventTypes,
			Enabled:     enabled,
		}
		if err := svc.CreateConfig(c.Context(), claims.TenantID, cfg); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"ok": true})
	}
}

func deleteAlertConfig(svc AlertPushServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		id := c.Params("id")
		if err := svc.DeleteConfig(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"deleted": id})
	}
}

func sendTestAlert(svc AlertPushServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		event := services.AlertEvent{
			TenantID:  claims.TenantID,
			EventType: "budget_warning",
			Title:     "Test Alert",
			Message:   "This is a test alert from ToTra admin.",
			Severity:  "info",
			Timestamp: time.Now(),
			Metadata:  map[string]any{"test": true},
		}
		if err := svc.Deliver(c.Context(), event); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	}
}
