package api

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUsageRoutes(app fiber.Router, svc services.UsageServiceInterface) {
	app.Get("/api/usage/summary", getMonthlySummary(svc))
	app.Get("/api/usage/adoption", getAdoptionRate(svc))
}

func getMonthlySummary(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		summaries, err := svc.GetMonthlySummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"month": month, "summaries": summaries})
	}
}

func getAdoptionRate(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required"})
		}
		stats, err := svc.GetAdoptionRate(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(stats)
	}
}

func RegisterUsageAdminRoutes(app fiber.Router, svc *services.UsageService) {
	app.Get("/api/admin/usage/department", getDepartmentSummary(svc))
	app.Get("/api/admin/usage/department/export", exportDepartmentCSV(svc))
	app.Get("/api/admin/usage/forecast", getBudgetForecast(svc))
	app.Get("/api/admin/usage/inactive", getInactiveUsers(svc))
}

func getDepartmentSummary(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		summaries, err := svc.GetDepartmentSummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"month": month, "departments": summaries})
	}
}

func exportDepartmentCSV(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		summaries, err := svc.GetDepartmentSummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		w.Write([]string{"Department", "Total Users", "Active Users", "SCU", "USD Cost", "Requests"})
		for _, d := range summaries {
			w.Write([]string{
				d.Department,
				strconv.Itoa(d.UserCount),
				strconv.Itoa(d.ActiveUsers),
				strconv.FormatFloat(d.TotalSCU, 'f', 2, 64),
				strconv.FormatFloat(d.TotalUSD, 'f', 4, 64),
				strconv.Itoa(d.RequestCount),
			})
		}
		w.Flush()
		c.Set("Content-Type", "text/csv; charset=utf-8")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cost-chargeback-%s.csv"`, month))
		return c.SendString(buf.String())
	}
}

func getBudgetForecast(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		forecast, err := svc.GetBudgetForecast(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(forecast)
	}
}

func getInactiveUsers(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		maxDays := 3
		if v := c.QueryInt("max_days", 3); v > 0 {
			maxDays = v
		}
		users, err := svc.GetInactiveUsers(c.Context(), claims.TenantID, month, maxDays)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"month": month, "max_days": maxDays, "users": users})
	}
}
