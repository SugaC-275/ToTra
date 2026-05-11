package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type AuditServiceIface interface {
	GetAuditLog(ctx context.Context, tenantID string, limit int) ([]*services.AuditEntry, error)
	VerifyChain(ctx context.Context, tenantID string) (*services.VerifyResult, error)
}

func RegisterAuditRoutes(app fiber.Router, svc AuditServiceIface) {
	app.Get("/api/admin/audit-log", getAuditLog(svc))
	app.Get("/api/admin/audit-log/verify", verifyAuditChain(svc))
}

func getAuditLog(svc AuditServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		limit := 50
		if l := c.Query("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		entries, err := svc.GetAuditLog(c.Context(), claims.TenantID, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if entries == nil {
			entries = []*services.AuditEntry{}
		}
		return c.JSON(entries)
	}
}

func verifyAuditChain(svc AuditServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := svc.VerifyChain(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}
