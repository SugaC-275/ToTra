package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type BudgetForecastServiceIface interface {
	GetBudgetForecast(ctx context.Context, tenantID string) (*services.EOYBudgetForecast, error)
}

type CostBenchmarkServiceIface interface {
	GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.CostBenchmark, error)
	GetModelROI(ctx context.Context) ([]services.ModelROI, error)
}

func RegisterCostAdvancedRoutes(r fiber.Router, budgetSvc BudgetForecastServiceIface, benchmarkSvc CostBenchmarkServiceIface) {
	r.Get("/api/admin/cost/budget-forecast", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := budgetSvc.GetBudgetForecast(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/benchmark", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := benchmarkSvc.GetCostBenchmark(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/model-roi", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := benchmarkSvc.GetModelROI(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})
}
