package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ProcurementReportServiceIface interface {
	GetProcurementReport(ctx context.Context, tenantID string, periodMonths int) (*services.ProcurementReport, error)
}

func RegisterProcurementRoutes(r fiber.Router, svc ProcurementReportServiceIface) {
	r.Get("/api/admin/cost/procurement-report", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		months := 6
		if m := c.Query("months"); m != "" {
			if v, err := strconv.Atoi(m); err == nil && v > 0 && v <= 24 {
				months = v
			}
		}
		report, err := svc.GetProcurementReport(c.Context(), claims.TenantID, months)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(report)
	})
}
