package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type TopSpendersServiceIface interface {
	GetTopSpenders(ctx context.Context, tenantID, yearMonth string, limit int) (*services.TopSpendersReport, error)
}

type BudgetAlertServiceIface interface {
	CheckAndNotify(ctx context.Context, tenantID string, bot services.BotBroadcaster) error
}

type BotBroadcasterIface interface {
	BroadcastAlert(ctx context.Context, tenantID, message string) error
}

func RegisterCostReportsRoutes(r fiber.Router, topSvc TopSpendersServiceIface, alertSvc BudgetAlertServiceIface, botSvc BotBroadcasterIface) {
	r.Get("/api/admin/cost/top-spenders", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		limit := 10
		if l := c.Query("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				limit = v
			}
		}
		report, err := topSvc.GetTopSpenders(c.Context(), claims.TenantID, month, limit)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(report)
	})

	r.Post("/api/admin/cost/budget-alert/check", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		if err := alertSvc.CheckAndNotify(c.Context(), claims.TenantID, botSvc); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"ok": true})
	})
}
