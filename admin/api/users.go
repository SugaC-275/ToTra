package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUserRoutes(app fiber.Router, svc services.UserServiceInterface) {
	app.Get("/api/users", listUsers(svc))
	app.Post("/api/users", createUser(svc))
	app.Get("/api/me/profile", getMyProfile(svc))
	app.Patch("/api/me/profile", updateMyProfile(svc))
}

func listUsers(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		users, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
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
			return serverError(c, err)
		}
		return c.Status(201).JSON(fiber.Map{
			"id": user.ID, "name": user.Name, "email": user.Email,
			"role": user.Role, "api_key": user.APIKey,
		})
	}
}

func getMyProfile(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		users, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		for _, u := range users {
			if u.ID == claims.UserID {
				return c.JSON(fiber.Map{"job_role": u.JobRole, "department": u.Department})
			}
		}
		return c.Status(404).JSON(fiber.Map{"error": "user not found"})
	}
}

func updateMyProfile(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.UpdateProfileRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if err := svc.UpdateProfile(c.Context(), claims.UserID, req); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}
