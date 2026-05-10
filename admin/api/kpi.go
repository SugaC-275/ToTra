package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterKPIRoutes(app fiber.Router, svc *services.KPIService) {
	app.Get("/api/kpi/snapshots", getKPISnapshots(svc))
	app.Get("/api/kpi/user-history", getKPIUserHistory(svc))
	app.Get("/api/me/kpi", getMyKPI(svc))
	app.Get("/api/me/kpi/submetrics", getMyKPISubmetrics(svc))
	app.Post("/api/admin/kpi/run", triggerKPISnapshot(svc))
	app.Get("/api/admin/kpi/anomalies", getKPIAnomalies(svc))
}

func getKPISnapshots(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", "")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required (?month=YYYY-MM)"})
		}
		snapshots, err := svc.GetSnapshots(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "snapshots": snapshots})
	}
}

func getKPIUserHistory(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		userID := c.Query("user_id", "")
		if userID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user_id required"})
		}
		snapshots, err := svc.GetUserHistory(c.Context(), claims.TenantID, userID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"snapshots": snapshots})
	}
}

func getMyKPI(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		snapshots, err := svc.GetMySnapshots(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"snapshots": snapshots})
	}
}

func getKPIAnomalies(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", "")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		anomalies, err := svc.GetAnomalies(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "anomalies": anomalies})
	}
}

func triggerKPISnapshot(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", "")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required (?month=2026-04)"})
		}
		if err := svc.RunMonthlySnapshot(c.Context(), claims.TenantID, month); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok", "month": month})
	}
}

func getMyKPISubmetrics(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		metrics, err := svc.GetUserAIQMetrics(c.Context(), claims.TenantID, claims.UserID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if metrics == nil {
			return c.JSON(fiber.Map{"month": month, "metrics": nil})
		}
		return c.JSON(fiber.Map{
			"month": month,
			"metrics": fiber.Map{
				"output_density":    metrics.OutputDensity,
				"usage_consistency": metrics.UsageConsistency,
				"task_depth":        metrics.TaskDepth,
				"cost_efficiency":   metrics.CostEfficiency,
				"active_days":       metrics.ActiveDays,
				"working_days":      services.WorkingDaysInMonth(month),
			},
		})
	}
}
