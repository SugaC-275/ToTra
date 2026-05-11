package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ROIServiceIface interface {
	GetQuarterlyROI(ctx context.Context, tenantID, year string) ([]*services.QuarterROI, error)
	GetBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.BenchmarkResult, error)
	GetDeptChallenge(ctx context.Context, tenantID, yearMonth string) ([]*services.DeptChallengeEntry, error)
}

func RegisterROIRoutes(app fiber.Router, svc ROIServiceIface) {
	app.Get("/api/admin/roi/quarterly", getQuarterlyROI(svc))
	app.Get("/api/admin/roi/benchmark", getROIBenchmark(svc))
	app.Get("/api/admin/roi/challenge", getDeptChallenge(svc))
}

func getQuarterlyROI(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		year := c.Query("year", time.Now().UTC().Format("2006"))
		result, err := svc.GetQuarterlyROI(c.Context(), claims.TenantID, year)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if result == nil {
			result = []*services.QuarterROI{}
		}
		return c.JSON(result)
	}
}

func getROIBenchmark(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		result, err := svc.GetBenchmark(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}

func getDeptChallenge(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		result, err := svc.GetDeptChallenge(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if result == nil {
			result = []*services.DeptChallengeEntry{}
		}
		return c.JSON(result)
	}
}
