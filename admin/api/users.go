package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUserRoutes(app fiber.Router, svc services.UserServiceInterface) {
	app.Get("/api/users", listUsers(svc))
	app.Post("/api/users", createUser(svc))
}

func listUsers(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		users, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(users), "users": users})
	}
}

func createUser(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.CreateUserRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Name == "" || req.Email == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name and email are required"})
		}
		if req.Role == "" {
			req.Role = "standard"
		}
		user, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{
			"id":      user.ID,
			"name":    user.Name,
			"email":   user.Email,
			"role":    user.Role,
			"api_key": user.APIKey,
		})
	}
}
