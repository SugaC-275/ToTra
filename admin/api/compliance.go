package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ComplianceServiceIface interface {
	GetViolations(ctx context.Context, tenantID, yearMonth string, limit int) ([]*services.ViolationRecord, error)
	GetRiskScores(ctx context.Context, tenantID, yearMonth string) ([]*services.UserRiskScore, error)
	GetMonthlyReport(ctx context.Context, tenantID, yearMonth string) (*services.ComplianceReport, error)
}

func RegisterComplianceRoutes(app fiber.Router, svc *services.ComplianceService) {
	app.Get("/api/admin/compliance/violations", getComplianceViolations(svc))
	app.Get("/api/admin/compliance/risk-scores", getComplianceRiskScores(svc))
	app.Get("/api/admin/compliance/report", getComplianceReport(svc))
}

func getComplianceViolations(svc *services.ComplianceService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		limit := c.QueryInt("limit", 100)
		if limit > 500 {
			limit = 500
		}
		data, err := svc.GetViolations(c.Context(), claims.TenantID, month, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if data == nil {
			data = []*services.ViolationRecord{}
		}
		return c.JSON(data)
	}
}

func getComplianceRiskScores(svc *services.ComplianceService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		data, err := svc.GetRiskScores(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if data == nil {
			data = []*services.UserRiskScore{}
		}
		return c.JSON(data)
	}
}

func getComplianceReport(svc *services.ComplianceService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		data, err := svc.GetMonthlyReport(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(data)
	}
}

// RegisterComplianceInternalRoutes registers internal (service-to-service) compliance endpoints.
// These are mounted on the root app (not the JWT-protected group).
func RegisterComplianceInternalRoutes(app *fiber.App, svc *services.ComplianceService, botSvc *services.BotService, internalSecret string) {
	app.Post("/internal/compliance/pii-alert", handlePIIAlert(botSvc, internalSecret))
}

func handlePIIAlert(botSvc *services.BotService, secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Get("X-Internal-Secret") != secret {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		var payload struct {
			TenantID    string `json:"tenant_id"`
			UserID      string `json:"user_id"`
			PIIType     string `json:"pii_type"`
			RequestPath string `json:"request_path"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad request"})
		}
		msg := fmt.Sprintf("⚠️ PII 违规告警\n类型: %s\n用户: %s\n路径: %s",
			payload.PIIType, payload.UserID, payload.RequestPath)
		_ = botSvc.BroadcastAlert(c.Context(), payload.TenantID, msg)
		return c.SendStatus(204)
	}
}

func currentYearMonth() string {
	return time.Now().UTC().Format("2006-01")
}
