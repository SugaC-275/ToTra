package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// RegisterWebhookRoutes mounts outbound webhook management endpoints.
func RegisterWebhookRoutes(router fiber.Router, store *storage.WebhookStore, dispatcher *WebhookDispatcher) {
	router.Post("/webhooks", createWebhook(store))
	router.Get("/webhooks", listWebhooks(store))
	router.Delete("/webhooks/:id", deleteWebhook(store))
	router.Post("/webhooks/:id/test", testWebhook(dispatcher))
}

func createWebhook(store *storage.WebhookStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		var req struct {
			Name        string   `json:"name"`
			URL         string   `json:"url"`
			Secret      string   `json:"secret"`
			Events      []string `json:"events"`
			WebhookType string   `json:"webhook_type"` // 'webhook' | 'slack' | 'pagerduty'
		}
		if err := c.BodyParser(&req); err != nil || req.Name == "" || req.URL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "name and url are required"},
			})
		}
		// For generic webhooks a secret is required for HMAC signing;
		// Slack and PagerDuty use their own auth mechanisms.
		if req.WebhookType == "" || req.WebhookType == "webhook" {
			if req.Secret == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": fiber.Map{"message": "secret is required for webhook type"},
				})
			}
		}
		if len(req.Events) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "at least one event type is required"},
			})
		}
		cfg, err := store.Create(c.Context(), user.TenantID, req.Name, req.URL, req.Secret, req.Events, req.WebhookType)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		// Omit secret from response.
		cfg.Secret = ""
		return c.Status(fiber.StatusCreated).JSON(cfg)
	}
}

func listWebhooks(store *storage.WebhookStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		cfgs, err := store.List(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if cfgs == nil {
			cfgs = []*storage.WebhookConfig{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": cfgs})
	}
}

func deleteWebhook(store *storage.WebhookStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		if err := store.Delete(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

func testWebhook(dispatcher *WebhookDispatcher) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if err := dispatcher.sendTestPing(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ping sent"})
	}
}
