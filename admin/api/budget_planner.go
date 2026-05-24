package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type BudgetPlannerServiceIface interface {
	GetBudgetPlan(ctx context.Context, tenantID string, planYear int) (*services.BudgetPlan, error)
}

func RegisterBudgetPlannerRoutes(r fiber.Router, svc BudgetPlannerServiceIface) {
	r.Get("/api/admin/cost/budget-plan", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		year := 0
		if y := c.Query("year"); y != "" {
			if v, err := strconv.Atoi(y); err == nil && v >= 2020 && v <= 2040 {
				year = v
			}
		}
		plan, err := svc.GetBudgetPlan(c.Context(), claims.TenantID, year)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(plan)
	})
}
