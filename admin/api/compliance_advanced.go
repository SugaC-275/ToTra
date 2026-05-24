package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type AnomalyServiceIface interface {
	GetAnomalies(ctx context.Context, tenantID string) ([]*services.AnomalyUser, error)
}

type ComplianceBenchmarkServiceIface interface {
	GetComplianceBenchmark(ctx context.Context, tenantID string) (*services.ComplianceBenchmark, error)
}

func RegisterComplianceAdvancedRoutes(app fiber.Router, anomalySvc AnomalyServiceIface, benchmarkSvc ComplianceBenchmarkServiceIface) {
	app.Get("/api/admin/compliance/anomalies", getComplianceAnomalies(anomalySvc))
	app.Get("/api/admin/compliance/benchmark", getComplianceBenchmark(benchmarkSvc))
}

func getComplianceAnomalies(svc AnomalyServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		anomalies, err := svc.GetAnomalies(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"anomalies": anomalies})
	}
}

func getComplianceBenchmark(svc ComplianceBenchmarkServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		bm, err := svc.GetComplianceBenchmark(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(bm)
	}
}
