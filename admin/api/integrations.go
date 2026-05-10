package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

func RegisterIntegrationRoutes(app fiber.Router, svc *services.IntegrationService, encKey string) {
	app.Get("/api/integrations", listWebhookConfigs(svc))
	app.Post("/api/integrations", createWebhookConfig(svc, encKey))
	app.Get("/api/me/integrations", listMyIntegrations(svc))
	app.Post("/api/me/integrations", bindMyIntegration(svc))
}

func listWebhookConfigs(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		configs, err := svc.ListWebhookConfigs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"configs": configs})
	}
}

func createWebhookConfig(svc *services.IntegrationService, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var req services.CreateWebhookConfigRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Platform == "" || req.WebhookSecret == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and webhook_secret required"})
		}
		encSecret, err := crypto.Encrypt(req.WebhookSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "encryption failed"})
		}
		config, err := svc.CreateWebhookConfig(c.Context(), claims.TenantID, encSecret, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(config)
	}
}

func listMyIntegrations(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		items, err := svc.ListUserIntegrations(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"integrations": items})
	}
}

func bindMyIntegration(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req struct {
			Platform   string `json:"platform"`
			ExternalID string `json:"external_id"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Platform == "" || req.ExternalID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and external_id required"})
		}
		createdBy := "employee"
		if claims.Role == "admin" {
			createdBy = "admin"
		}
		if err := svc.BindUserIntegration(c.Context(), claims.TenantID, claims.UserID, req.Platform, req.ExternalID, createdBy); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"status": "bound"})
	}
}
