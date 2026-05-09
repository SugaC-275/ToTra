package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type UserInfo struct {
	UserID   string
	TenantID string
	Role     string
}

type UserLookup interface {
	LookupByKeyHash(hash string) (*UserInfo, error)
}

func NewAuthMiddleware(lookup UserLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "missing Authorization header", "type": "auth_error"},
			})
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid Authorization format", "type": "auth_error"},
			})
		}

		keyHash := hashKey(token)
		user, err := lookup.LookupByKeyHash(keyHash)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "auth lookup failed", "type": "server_error"},
			})
		}
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid API key", "type": "auth_error"},
			})
		}

		c.Locals("user", user)
		return c.Next()
	}
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
