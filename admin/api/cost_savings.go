package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type CostSavingsServiceIface interface {
	GetMonthlySavings(ctx context.Context, tenantID, yearMonth string) (*services.MonthlySavingsReport, error)
}

func RegisterCostSavingsRoutes(app fiber.Router, svc CostSavingsServiceIface) {
	app.Get("/api/admin/cost/savings-report", getCostSavingsReport(svc))
	app.Get("/api/admin/cost/savings", getCostSavingsReport(svc))
}

func getCostSavingsReport(svc CostSavingsServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		report, err := svc.GetMonthlySavings(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(report)
	}
}
