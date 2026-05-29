package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// RegisterVirtualKeyRoutes mounts virtual key CRUD at /admin/virtual-keys.
func RegisterVirtualKeyRoutes(router fiber.Router, store *storage.VirtualKeyStore) {
	router.Post("/virtual-keys", createVirtualKey(store))
	router.Get("/virtual-keys", listVirtualKeys(store))
	router.Delete("/virtual-keys/:id", revokeVirtualKey(store))
}

func createVirtualKey(store *storage.VirtualKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		var req struct {
			Name       string    `json:"name"`
			ModelAlias string    `json:"model_alias"`
			BudgetUSD  *float64  `json:"budget_usd"`
			PIIPolicy  string    `json:"pii_policy"`
			BundleIDs  []string  `json:"bundle_ids"`
			ExpiresAt  *time.Time `json:"expires_at"`
		}
		if err := c.BodyParser(&req); err != nil || req.Name == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "name is required"},
			})
		}

		// Generate a cryptographically random raw key (32 bytes = 64 hex chars).
		rawBytes := make([]byte, 32)
		if _, err := rand.Read(rawBytes); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "key generation failed"})
		}
		rawKey := "vk-" + hex.EncodeToString(rawBytes)

		sum := sha256.Sum256([]byte(rawKey))
		keyHash := hex.EncodeToString(sum[:])

		piiPolicy := req.PIIPolicy
		if piiPolicy == "" {
			piiPolicy = "inherit"
		}
		bundleIDs := req.BundleIDs
		if bundleIDs == nil {
			bundleIDs = []string{}
		}

		vk := &storage.VirtualKey{
			TenantID:  user.TenantID,
			Name:      req.Name,
			KeyHash:   keyHash,
			BudgetUSD: req.BudgetUSD,
			PIIPolicy: piiPolicy,
			BundleIDs: bundleIDs,
			ExpiresAt: req.ExpiresAt,
			CreatedBy: user.UserID,
		}
		if req.ModelAlias != "" {
			vk.ModelAlias = &req.ModelAlias
		}

		if err := store.Create(c.Context(), vk); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		// Return the raw key exactly once — never stored, not retrievable again.
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"id":          vk.ID,
			"name":        vk.Name,
			"key":         rawKey, // shown once
			"model_alias": vk.ModelAlias,
			"budget_usd":  vk.BudgetUSD,
			"pii_policy":  vk.PIIPolicy,
			"bundle_ids":  vk.BundleIDs,
			"expires_at":  vk.ExpiresAt,
			"created_at":  vk.CreatedAt,
		})
	}
}

func listVirtualKeys(store *storage.VirtualKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		keys, err := store.List(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if keys == nil {
			keys = []*storage.VirtualKey{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": keys})
	}
}

func revokeVirtualKey(store *storage.VirtualKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		if err := store.Revoke(c.Context(), c.Params("id"), user.TenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error()},
			})
		}
		return c.JSON(fiber.Map{"status": "revoked"})
	}
}
