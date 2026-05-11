package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type AIInsightsServiceIface interface {
	GenerateInsight(ctx context.Context, userID, month string) (string, error)
}

func RegisterAIInsightsRoutes(app fiber.Router, svc AIInsightsServiceIface) {
	app.Get("/api/me/kpi/insights", getKPIInsights(svc))
}

func getKPIInsights(svc AIInsightsServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.UserID == "" {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		month := c.Query("month")
		if month == "" {
			month = time.Now().UTC().Format("2006-01")
		}
		insight, err := svc.GenerateInsight(c.Context(), claims.UserID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"insight": insight, "month": month})
	}
}
