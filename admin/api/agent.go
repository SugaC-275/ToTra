package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// AgentServiceInterface is the narrow interface the handler needs.
type AgentServiceInterface interface {
	GetAgentSessions(ctx context.Context, tenantID, yearMonth string) ([]*services.AgentSession, error)
	GetMyAgentSessions(ctx context.Context, userID, yearMonth string) ([]*services.AgentSession, error)
}

// RegisterAgentRoutes wires the two agent-session endpoints onto the router.
func RegisterAgentRoutes(app fiber.Router, svc AgentServiceInterface) {
	app.Get("/api/admin/agent-sessions", getAdminAgentSessions(svc))
	app.Get("/api/me/agent-sessions", getMyAgentSessions(svc))
}

func getAdminAgentSessions(svc AgentServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		sessions, err := svc.GetAgentSessions(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		if sessions == nil {
			sessions = []*services.AgentSession{}
		}
		return c.JSON(fiber.Map{"month": month, "sessions": sessions})
	}
}

func getMyAgentSessions(svc AgentServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		sessions, err := svc.GetMyAgentSessions(c.Context(), claims.UserID, month)
		if err != nil {
			return serverError(c, err)
		}
		if sessions == nil {
			sessions = []*services.AgentSession{}
		}
		return c.JSON(fiber.Map{"month": month, "sessions": sessions})
	}
}
