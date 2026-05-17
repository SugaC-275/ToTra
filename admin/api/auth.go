package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/services"
	"golang.org/x/crypto/bcrypt"
)

func LoginHandler(pool *pgxpool.Pool, jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Email == "" || body.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "email and password required"})
		}

		var userID, tenantID, role string
		var passwordHash *string
		err := pool.QueryRow(context.Background(),
			`SELECT id, tenant_id, role, password_hash FROM users WHERE email = $1 AND is_active = true`,
			body.Email,
		).Scan(&userID, &tenantID, &role, &passwordHash)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}

		if passwordHash == nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}
		if err := bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte(body.Password)); err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}

		token, err := jwtSvc.Issue(userID, tenantID, role)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "token generation failed"})
		}
		return c.JSON(fiber.Map{"token": token})
	}
}
