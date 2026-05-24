package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type RiskTrendServiceIface interface {
	GetRiskTrend(ctx context.Context, tenantID string) (*services.RiskTrend, error)
}

type ComplianceDigestServiceIface interface {
	GetDigest(ctx context.Context, tenantID, yearMonth string) (*services.ComplianceDigest, error)
}

func RegisterComplianceReportRoutes(r fiber.Router, trendSvc RiskTrendServiceIface, digestSvc ComplianceDigestServiceIface) {
	r.Get("/api/admin/compliance/risk-trend", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := trendSvc.GetRiskTrend(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/compliance/digest", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := digestSvc.GetDigest(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(result)
	})
}
