package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

type webhookServiceIface interface {
	GetWebhookConfig(ctx context.Context, tenantID, platform string) (string, map[string]float64, error)
	MatchUser(ctx context.Context, tenantID string, event *services.ParsedEvent) (string, error)
	SaveEvent(ctx context.Context, tenantID, userID string, event *services.ParsedEvent, rawPayload []byte) error
}

func RegisterWebhookRoutes(app fiber.Router, svc webhookServiceIface, encryptionKey string) {
	app.Post("/webhooks/github", githubWebhook(svc, encryptionKey))
	app.Post("/webhooks/jira", jiraWebhook(svc, encryptionKey))
	app.Post("/webhooks/feishu", feishuWebhook(svc, encryptionKey))
	app.Post("/webhooks/gitlab", gitlabWebhook(svc, encryptionKey))
	app.Post("/webhooks/confluence", confluenceWebhook(svc, encryptionKey))
}

func githubWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "github")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "github integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitHubSignature(body, secret, c.Get("X-Hub-Signature-256")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		eventType := c.Get("X-GitHub-Event")
		event, err := services.ParseGitHubEvent(eventType, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("github", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return serverError(c, err)
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func jiraWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "jira")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "jira integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyJiraSignature(body, secret, c.Get("X-Hub-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		event, err := services.ParseJiraEvent(body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("jira", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return serverError(c, err)
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func feishuWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "feishu")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "feishu integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		timestamp := c.Get("X-Lark-Request-Timestamp")
		if !services.VerifyFeishuSignature(body, secret, timestamp, c.Get("X-Lark-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		event, err := services.ParseFeishuEvent(body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("feishu", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return serverError(c, err)
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func gitlabWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "gitlab")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "gitlab integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitLabToken(secret, c.Get("X-Gitlab-Token")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid token"})
		}
		eventType := c.Get("X-Gitlab-Event")
		event, err := services.ParseGitLabEvent(eventType, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("gitlab", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return serverError(c, err)
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func confluenceWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "confluence")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "confluence integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitHubSignature(body, secret, c.Get("X-Hub-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		eventKey := c.Get("X-Event-Key")
		event, err := services.ParseConfluenceEvent(eventKey, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("confluence", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return serverError(c, err)
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}
