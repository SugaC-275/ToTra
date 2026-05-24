package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type BotServiceIface interface {
	List(ctx context.Context, tenantID string) ([]services.BotConfig, error)
	Add(ctx context.Context, tenantID, platform, webhookURL, label string) (*services.BotConfig, error)
	Delete(ctx context.Context, tenantID, id string) error
	SendTestMessage(ctx context.Context, tenantID, id string) error
}

func RegisterBotRoutes(app fiber.Router, svc BotServiceIface) {
	app.Get("/api/admin/bot-configs", listBotConfigs(svc))
	app.Post("/api/admin/bot-configs", addBotConfig(svc))
	app.Delete("/api/admin/bot-configs/:id", deleteBotConfig(svc))
	app.Post("/api/admin/bot-configs/:id/test", sendBotTestMessage(svc))
}

func listBotConfigs(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		configs, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		if configs == nil {
			configs = []services.BotConfig{}
		}
		return c.JSON(fiber.Map{"configs": configs})
	}
}

func addBotConfig(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Platform   string `json:"platform"`
			WebhookURL string `json:"webhook_url"`
			Label      string `json:"label"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Platform == "" || body.WebhookURL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and webhook_url are required"})
		}
		cfg, err := svc.Add(c.Context(), claims.TenantID, body.Platform, body.WebhookURL, body.Label)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(cfg)
	}
}

func deleteBotConfig(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

func sendBotTestMessage(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.SendTestMessage(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}
