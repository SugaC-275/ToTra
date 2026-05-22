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

// RequireRole allows access when the JWT role matches the required role or is "admin".
// Behaviour is unchanged from the original implementation.
func RequireRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != role && claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "insufficient permissions"})
		}
		return c.Next()
	}
}

// RequireAnyRole allows access when the user holds at least one of the specified roles.
// It checks the JWT claims.Role first (fast path) and then queries the user_roles table
// via RBACServiceIface for supplemental fine-grained roles.
func RequireAnyRole(svc services.RBACServiceIface, roles ...string) fiber.Handler {
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r] = struct{}{}
	}
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)

		// admin JWT role always passes.
		if claims.Role == "admin" {
			return c.Next()
		}

		// Fast path: JWT claim matches one of the required roles directly.
		if _, ok := roleSet[claims.Role]; ok {
			return c.Next()
		}

		// Supplemental check: query user_roles table.
		for role := range roleSet {
			has, err := svc.HasRole(c.Context(), claims.TenantID, claims.UserID, services.Role(role))
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "role check failed"})
			}
			if has {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{"error": "insufficient permissions"})
	}
}
