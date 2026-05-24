package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterGDPRRoutes(
	app fiber.Router,
	retSvc services.DataRetentionServiceIface,
	delSvc services.DeletionRequestServiceIface,
) {
	app.Get("/api/admin/data-retention", getDataRetention(retSvc))
	app.Put("/api/admin/data-retention", putDataRetention(retSvc))
	app.Post("/api/admin/data-retention/run", runRetentionCleanup(retSvc))
	app.Get("/api/admin/data-deletion-requests", listDeletionRequests(delSvc))
	app.Post("/api/admin/data-deletion-requests/:id/approve", approveDeletionRequest(delSvc))
	app.Post("/api/admin/data-deletion-requests/:id/reject", rejectDeletionRequest(delSvc))
	app.Get("/api/me/data-export", exportMyData(delSvc))
	app.Post("/api/me/data-deletion-request", createDeletionRequest(delSvc))
}

func getDataRetention(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		months, err := svc.GetRetentionMonths(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"data_retention_months": months})
	}
}

func putDataRetention(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Months int `json:"data_retention_months"`
		}
		if err := c.BodyParser(&body); err != nil || body.Months < 1 {
			return c.Status(400).JSON(fiber.Map{"error": "data_retention_months must be >= 1"})
		}
		if err := svc.SetRetentionMonths(c.Context(), claims.TenantID, body.Months); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"data_retention_months": body.Months})
	}
}

func runRetentionCleanup(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		deleted, err := svc.RunRetentionCleanup(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"deleted_count": deleted})
	}
}

func listDeletionRequests(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		reqs, err := svc.ListPending(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		if reqs == nil {
			reqs = []*services.DeletionRequest{}
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

func approveDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Approve(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "approved"})
	}
}

func rejectDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Reject(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "rejected"})
	}
}

func exportMyData(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		export, err := svc.ExportUserData(c.Context(), claims.TenantID, claims.UserID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(export)
	}
}

func createDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		req, err := svc.CreateRequest(c.Context(), claims.TenantID, claims.UserID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(req)
	}
}
