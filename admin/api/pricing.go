package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// PricingSyncServiceIface is the interface the pricing handlers depend on.
type PricingSyncServiceIface interface {
	TriggerSync(ctx context.Context) (int, error)
	GetCurrentPricing(ctx context.Context) ([]*services.ModelPricingRow, error)
	GetPricingHistory(ctx context.Context, modelName string) ([]*services.ModelPricingRow, error)
}

// RegisterPricingRoutes mounts model pricing endpoints.
//
//	POST /api/pricing/sync             — admin-triggered pricing sync
//	GET  /api/pricing                  — current pricing table
//	GET  /api/pricing/:model/history   — price history for a model
func RegisterPricingRoutes(r fiber.Router, svc PricingSyncServiceIface) {
	r.Post("/api/pricing/sync", triggerPricingSync(svc))
	r.Get("/api/pricing", getCurrentPricing(svc))
	r.Get("/api/pricing/:model/history", getPricingHistory(svc))
}

func triggerPricingSync(svc PricingSyncServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		updated, err := svc.TriggerSync(c.Context())
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{
			"status":  "ok",
			"updated": updated,
		})
	}
}

func getCurrentPricing(svc PricingSyncServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		_ = claims
		pricing, err := svc.GetCurrentPricing(c.Context())
		if err != nil {
			return serverError(c, err)
		}
		if pricing == nil {
			pricing = []*services.ModelPricingRow{}
		}
		return c.JSON(pricing)
	}
}

func getPricingHistory(svc PricingSyncServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		_ = claims
		modelName := c.Params("model")
		history, err := svc.GetPricingHistory(c.Context(), modelName)
		if err != nil {
			return serverError(c, err)
		}
		if history == nil {
			history = []*services.ModelPricingRow{}
		}
		return c.JSON(history)
	}
}
