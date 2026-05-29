package api

import (
	"context"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// BAAStoreIface is the interface the BAA handler depends on.
type BAAStoreIface interface {
	Sign(ctx context.Context, tenantID, userID, name, email, version, ip string) (*services.BAARecord, error)
	GetActive(ctx context.Context, tenantID string) (*services.BAARecord, error)
	List(ctx context.Context, tenantID string) ([]*services.BAARecord, error)
	Revoke(ctx context.Context, tenantID, id string) error
}

// RegisterBAARoutes mounts BAA e-signing endpoints.
//
//	GET    /api/compliance/baa         — current BAA status
//	POST   /api/compliance/baa/sign    — sign BAA (admin only)
//	GET    /api/compliance/baa/history — all signed records (admin only)
//	DELETE /api/compliance/baa/:id     — revoke a record (admin only)
func RegisterBAARoutes(r fiber.Router, store BAAStoreIface) {
	r.Get("/api/compliance/baa", getBAAStatus(store))
	r.Post("/api/compliance/baa/sign", signBAA(store))
	r.Get("/api/compliance/baa/history", listBAAHistory(store))
	r.Delete("/api/compliance/baa/:id", revokeBAA(store))
}

func getBAAStatus(store BAAStoreIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		record, err := store.GetActive(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		if record == nil {
			return c.JSON(fiber.Map{"signed": false})
		}
		return c.JSON(fiber.Map{
			"signed":            true,
			"signed_at":         record.SignedAt,
			"signed_by":         record.SignedByName,
			"signed_by_email":   record.SignedByEmail,
			"agreement_version": record.AgreementVersion,
			"record_id":         record.ID,
		})
	}
}

func signBAA(store BAAStoreIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		var body struct {
			Name    string `json:"name"`
			Email   string `json:"email"`
			Version string `json:"version"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Email) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name and email are required"})
		}
		version := body.Version
		if version == "" {
			version = "v1.0"
		}
		ip := extractIP(c)
		record, err := store.Sign(c.Context(), claims.TenantID, claims.UserID, body.Name, body.Email, version, ip)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(fiber.StatusCreated).JSON(record)
	}
}

func listBAAHistory(store BAAStoreIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		records, err := store.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(records)
	}
}

func revokeBAA(store BAAStoreIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id required"})
		}
		if err := store.Revoke(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// extractIP returns the real client IP from X-Real-IP, X-Forwarded-For, or RemoteAddr.
func extractIP(c *fiber.Ctx) string {
	if ip := c.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	addr := c.IP()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
