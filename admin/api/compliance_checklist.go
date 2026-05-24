package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// ChecklistServiceIface is the interface for the EU AI Act checklist service.
type ChecklistServiceIface interface {
	GetChecklist(ctx context.Context, tenantID string) ([]*services.ChecklistEntry, error)
	UpdateItem(ctx context.Context, tenantID, itemKey, status, notes string) error
	ExportViolationsCSV(ctx context.Context, tenantID, yearMonth string) (string, error)
	GetSOC2Report(ctx context.Context, tenantID string) (*services.SOC2Report, error)
}

// RegisterChecklistRoutes mounts the EU AI Act checklist routes.
func RegisterChecklistRoutes(app fiber.Router, svc ChecklistServiceIface) {
	app.Get("/api/admin/compliance/checklist", getChecklist(svc))
	app.Put("/api/admin/compliance/checklist/:key", updateChecklistItem(svc))
	app.Get("/api/admin/compliance/audit-export", exportViolationsCSV(svc))
	app.Get("/api/admin/compliance/soc2-report", getSOC2Report(svc))
}

func getChecklist(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		items, err := svc.GetChecklist(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"items": items})
	}
}

func updateChecklistItem(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Status string `json:"status"`
			Notes  string `json:"notes"`
		}
		if err := c.BodyParser(&body); err != nil || body.Status == "" {
			return c.Status(400).JSON(fiber.Map{"error": "status required"})
		}
		if err := svc.UpdateItem(c.Context(), claims.TenantID, c.Params("key"), body.Status, body.Notes); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(204)
	}
}

func exportViolationsCSV(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		csv, err := svc.ExportViolationsCSV(c.Context(), claims.TenantID, month)
		if err != nil {
			return serverError(c, err)
		}
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", `attachment; filename="compliance-violations-`+month+`.csv"`)
		return c.SendString(csv)
	}
}

func getSOC2Report(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		report, err := svc.GetSOC2Report(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(report)
	}
}
