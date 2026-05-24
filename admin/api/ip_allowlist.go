package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type IPAllowlistServiceIface interface {
	List(ctx context.Context, tenantID string) ([]*services.IPAllowlistEntry, error)
	Add(ctx context.Context, tenantID, cidr, label string) (*services.IPAllowlistEntry, error)
	Delete(ctx context.Context, tenantID, id string) error
}

func RegisterIPAllowlistRoutes(app fiber.Router, svc IPAllowlistServiceIface) {
	app.Get("/api/admin/ip-allowlist", listIPAllowlist(svc))
	app.Post("/api/admin/ip-allowlist", addIPAllowlist(svc))
	app.Delete("/api/admin/ip-allowlist/:id", deleteIPAllowlist(svc))
}

func listIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		entries, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		if entries == nil {
			entries = []*services.IPAllowlistEntry{}
		}
		return c.JSON(fiber.Map{"entries": entries})
	}
}

func addIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			CIDR  string `json:"cidr"`
			Label string `json:"label"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if body.CIDR == "" {
			return c.Status(400).JSON(fiber.Map{"error": "cidr is required"})
		}
		entry, err := svc.Add(c.Context(), claims.TenantID, body.CIDR, body.Label)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(entry)
	}
}

func deleteIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(400).JSON(fiber.Map{"error": "id param required"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}
