package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUsageRoutes(app fiber.Router, svc services.UsageServiceInterface) {
	app.Get("/api/usage/summary", getMonthlySummary(svc))
	app.Get("/api/usage/adoption", getAdoptionRate(svc))
}

func getMonthlySummary(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		summaries, err := svc.GetMonthlySummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "summaries": summaries})
	}
}

func getAdoptionRate(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required"})
		}
		stats, err := svc.GetAdoptionRate(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(stats)
	}
}
