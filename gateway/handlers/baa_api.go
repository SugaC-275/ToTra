package handlers

import (
	"context"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// BAADataStore is the interface the gateway BAA handler depends on.
type BAADataStore interface {
	Sign(ctx context.Context, tenantID, userID, name, email, version, ip string) (*storage.BAARecord, error)
	GetActive(ctx context.Context, tenantID string) (*storage.BAARecord, error)
	List(ctx context.Context, tenantID string) ([]*storage.BAARecord, error)
	Revoke(ctx context.Context, tenantID, id string) error
}

// RegisterBAARoutes mounts BAA e-signing endpoints on the given router.
//
//	GET    /v1/compliance/baa          — current BAA status
//	POST   /v1/compliance/baa/sign     — sign BAA (admin only)
//	GET    /v1/compliance/baa/history  — all signed records (admin only)
//	DELETE /v1/compliance/baa/:id      — revoke a record (admin only)
func RegisterBAARoutes(router fiber.Router, store BAADataStore) {
	router.Get("/compliance/baa", getBAAStatusGateway(store))
	router.Post("/compliance/baa/sign", signBAAGateway(store))
	router.Get("/compliance/baa/history", listBAAHistoryGateway(store))
	router.Delete("/compliance/baa/:id", revokeBAAGateway(store))
}

func getBAAStatusGateway(store BAADataStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		record, err := store.GetActive(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
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

func signBAAGateway(store BAADataStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
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
		ip := extractClientIP(c)
		record, err := store.Sign(c.Context(), user.TenantID, user.UserID, body.Name, body.Email, version, ip)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(record)
	}
}

func listBAAHistoryGateway(store BAADataStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		records, err := store.List(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(records)
	}
}

func revokeBAAGateway(store BAADataStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id required"})
		}
		if err := store.Revoke(c.Context(), user.TenantID, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// extractClientIP returns the real client IP from X-Real-IP, X-Forwarded-For, or RemoteAddr.
func extractClientIP(c *fiber.Ctx) string {
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
