package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// RetentionCleanupMetaStore persists and retrieves last-cleanup metadata.
// Implementations store data in tenant_cost_settings (last_cleanup_at,
// last_cleanup_rows_deleted).
type RetentionCleanupMetaStore interface {
	GetCleanupMeta(ctx context.Context, tenantID string) (*RetentionCleanupMeta, error)
	SaveCleanupMeta(ctx context.Context, tenantID string, rowsDeleted int64, at time.Time) error
}

// RetentionCleanupMeta holds the result of the most recent cleanup run.
type RetentionCleanupMeta struct {
	LastCleanupAt          *time.Time `json:"last_cleanup_at"`           // null if never run
	LastCleanupRowsDeleted *int64     `json:"last_cleanup_rows_deleted"` // null if never run
}

// retentionResponse is the shape returned by GET /api/admin/data-retention.
type retentionResponse struct {
	RetentionMonths        int        `json:"retention_months"`
	LastCleanupAt          *time.Time `json:"last_cleanup_at"`
	LastCleanupRowsDeleted *int64     `json:"last_cleanup_rows_deleted"`
}

// RetentionSvcIface re-exports the service interface so tests can reference it
// from this package without importing services directly.
type RetentionSvcIface = services.DataRetentionServiceIface

// RegisterDataRetentionRoutes wires the three data-retention API endpoints.
// It is intentionally separate from gdpr.go to keep concerns isolated.
func RegisterDataRetentionRoutes(
	r fiber.Router,
	retSvc services.DataRetentionServiceIface,
	meta RetentionCleanupMetaStore,
) {
	r.Get("/api/admin/data-retention/config", getRetentionConfig(retSvc, meta))
	r.Put("/api/admin/data-retention/config", putRetentionConfig(retSvc))
	r.Post("/api/admin/data-retention/cleanup", postRetentionCleanup(retSvc, meta))
}

func getRetentionConfig(svc services.DataRetentionServiceIface, meta RetentionCleanupMetaStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}

		months, err := svc.GetRetentionMonths(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		m, err := meta.GetCleanupMeta(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		resp := retentionResponse{
			RetentionMonths: months,
		}
		if m != nil {
			resp.LastCleanupAt = m.LastCleanupAt
			resp.LastCleanupRowsDeleted = m.LastCleanupRowsDeleted
		}
		return c.JSON(resp)
	}
}

func putRetentionConfig(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}

		var body struct {
			Months int `json:"months"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if body.Months < 1 || body.Months > 84 {
			return c.Status(400).JSON(fiber.Map{"error": "months must be between 1 and 84"})
		}

		if err := svc.SetRetentionMonths(c.Context(), claims.TenantID, body.Months); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"retention_months": body.Months})
	}
}

func postRetentionCleanup(svc services.DataRetentionServiceIface, meta RetentionCleanupMetaStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}

		deleted, err := svc.RunRetentionCleanup(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		now := time.Now().UTC()
		if err := meta.SaveCleanupMeta(c.Context(), claims.TenantID, deleted, now); err != nil {
			// Non-fatal: log but still return success
			_ = err
		}

		return c.JSON(fiber.Map{
			"rows_deleted":    deleted,
			"last_cleanup_at": now.Format(time.RFC3339),
		})
	}
}
