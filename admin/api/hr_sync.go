package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type HRSyncServiceIface interface {
	SyncFromCSV(ctx context.Context, tenantID string, records []services.HRRecord) (*services.SyncResult, error)
}

func RegisterHRSyncRoutes(app fiber.Router, svc HRSyncServiceIface) {
	app.Post("/api/admin/hr/sync", hrSync(svc))
}

func hrSync(svc HRSyncServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "file is required"})
		}

		f, err := fileHeader.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to open file"})
		}
		defer f.Close()

		records, err := services.ParseHRCSV(f)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		result, err := svc.SyncFromCSV(c.Context(), claims.TenantID, records)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}
