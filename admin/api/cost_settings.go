package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type CostSettingsServiceIface interface {
	Get(ctx context.Context, tenantID string) (*services.CostSettings, error)
	Upsert(ctx context.Context, tenantID string, monthlyBudgetUSD *float64, workStartHour, workEndHour int) (*services.CostSettings, error)
}

type BudgetStatusServiceIface interface {
	GetBudgetStatus(ctx context.Context, tenantID string) (*services.BudgetStatus, error)
}

type OffHoursServiceIface interface {
	GetOffHoursReport(ctx context.Context, tenantID, yearMonth string) (*services.OffHoursReport, error)
}

func RegisterCostSettingsRoutes(r fiber.Router, settingsSvc CostSettingsServiceIface, budgetSvc BudgetStatusServiceIface, offHoursSvc OffHoursServiceIface) {
	r.Get("/api/admin/cost/settings", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := settingsSvc.Get(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(result)
	})

	r.Put("/api/admin/cost/settings", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
			WorkStartHour    int      `json:"work_start_hour"`
			WorkEndHour      int      `json:"work_end_hour"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		result, err := settingsSvc.Upsert(c.Context(), claims.TenantID, body.MonthlyBudgetUSD, body.WorkStartHour, body.WorkEndHour)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/budget-status", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := budgetSvc.GetBudgetStatus(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/off-hours", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := offHoursSvc.GetOffHoursReport(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(result)
	})
}
