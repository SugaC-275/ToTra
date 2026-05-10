package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterFuelRoutes(app fiber.Router, svc *services.FuelService) {
	app.Get("/api/fuel/settings", getFuelSettings(svc))
	app.Put("/api/fuel/settings", updateFuelSettings(svc))
	app.Get("/api/me/fuel", getMyFuel(svc))
}

func getFuelSettings(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		settings, err := svc.GetSettings(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(settings)
	}
}

func updateFuelSettings(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var req struct {
			Top10PctBonus float64 `json:"top_10pct_bonus"`
			Top25PctBonus float64 `json:"top_25pct_bonus"`
			Top50PctBonus float64 `json:"top_50pct_bonus"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if err := svc.UpdateSettings(c.Context(), claims.TenantID, req.Top10PctBonus, req.Top25PctBonus, req.Top50PctBonus); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func getMyFuel(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		txns, err := svc.GetMyTransactions(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"transactions": txns})
	}
}
