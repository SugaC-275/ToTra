package handlers

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// keyRotationStore is the subset of APIKeyStore used by the admin routes.
type keyRotationStore interface {
	ListForModel(ctx context.Context, modelConfigID string) ([]*storage.APIKeyRecord, error)
}

// RegisterKeyRotationRoutes mounts admin-only key rotation management endpoints.
//
//	POST   /key_rotation/keys                    — add a key to a model config
//	GET    /key_rotation/keys/:model_config_id   — list keys for a model config (redacted)
//	DELETE /key_rotation/keys/:key_id            — deactivate a key
func RegisterKeyRotationRoutes(router fiber.Router, store *storage.APIKeyStore) {
	router.Post("/key_rotation/keys", addKey(store))
	router.Get("/key_rotation/keys/:model_config_id", listKeys(store))
	router.Delete("/key_rotation/keys/:key_id", deactivateKey(store))
}

type addKeyPayload struct {
	ModelConfigID string `json:"model_config_id"`
	APIKey        string `json:"api_key"`
	Weight        int    `json:"weight"`
}

func addKey(store *storage.APIKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		var p addKeyPayload
		if err := c.BodyParser(&p); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if p.ModelConfigID == "" || p.APIKey == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "model_config_id and api_key are required"})
		}
		if p.Weight <= 0 {
			p.Weight = 1
		}

		id, err := store.AddKey(c.Context(), p.ModelConfigID, p.APIKey, p.Weight)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id, "status": "created"})
	}
}

func listKeys(store *storage.APIKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		modelConfigID := c.Params("model_config_id")
		if modelConfigID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "model_config_id is required"})
		}

		records, err := store.ListForModel(c.Context(), modelConfigID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		type redactedKey struct {
			ID            string  `json:"id"`
			ModelConfigID string  `json:"model_config_id"`
			APIKeyHint    string  `json:"api_key_hint"`
			Weight        int     `json:"weight"`
			ErrorCount    int     `json:"error_count"`
			CooldownUntil *string `json:"cooldown_until,omitempty"`
		}

		out := make([]redactedKey, 0, len(records))
		for _, r := range records {
			hint := redactKey(r.APIKey)
			rk := redactedKey{
				ID:            r.ID,
				ModelConfigID: r.ModelConfigID,
				APIKeyHint:    hint,
				Weight:        r.Weight,
				ErrorCount:    r.ErrorCount,
			}
			if r.CooldownUntil != nil {
				s := r.CooldownUntil.String()
				rk.CooldownUntil = &s
			}
			out = append(out, rk)
		}
		return c.JSON(fiber.Map{"keys": out})
	}
}

func deactivateKey(store *storage.APIKeyStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		keyID := c.Params("key_id")
		if keyID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key_id is required"})
		}

		if err := store.DeactivateKey(c.Context(), keyID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "deactivated"})
	}
}

// redactKey shows the first 4 and last 4 characters of a key for admin inspection.
func redactKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}
