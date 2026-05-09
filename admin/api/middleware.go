package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func NewJWTMiddleware(jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" {
			return c.Status(401).JSON(fiber.Map{"error": "missing Authorization header"})
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, err := jwtSvc.Verify(tokenStr)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid or expired token"})
		}
		c.Locals("claims", claims)
		return c.Next()
	}
}

func RequireRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != role && claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "insufficient permissions"})
		}
		return c.Next()
	}
}
