package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ModelServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error)
	Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error)
}

func RegisterModelRoutes(app fiber.Router, svc ModelServiceInterface) {
	app.Get("/api/models", listModels(svc))
	app.Post("/api/models", createModel(svc))
}

func listModels(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		models, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(models), "models": models})
	}
}

func createModel(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.CreateModelRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Name == "" || req.Provider == "" || req.BaseURL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name, provider, base_url required"})
		}
		model, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(model)
	}
}
