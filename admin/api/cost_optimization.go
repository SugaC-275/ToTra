package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type OptimizationSuggestionServiceIface interface {
	GetSuggestions(ctx context.Context, tenantID string) (*services.OptimizationReport, error)
}

func RegisterCostOptimizationRoutes(r fiber.Router, svc OptimizationSuggestionServiceIface) {
	r.Get("/api/admin/cost/optimization-suggestions", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		report, err := svc.GetSuggestions(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(report)
	})
}
