package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ModelServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error)
	Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error)
	UpdatePricing(ctx context.Context, tenantID, modelID string, inputPrice, outputPrice float64) (*services.ModelConfig, error)
	UpdateCacheSettings(ctx context.Context, tenantID, modelID string, cacheDisabled bool) (*services.ModelConfig, error)
}

func RegisterModelRoutes(app fiber.Router, svc ModelServiceInterface) {
	app.Get("/api/models", listModels(svc))
	app.Post("/api/models", createModel(svc))
	app.Put("/api/admin/models/:id/pricing", updateModelPricing(svc))
	app.Put("/api/admin/models/:id/cache", updateModelCache(svc))
}

func listModels(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		models, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
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
			return serverError(c, err)
		}
		return c.Status(201).JSON(model)
	}
}

type updateCacheBody struct {
	CacheDisabled bool `json:"cache_disabled"`
}

func updateModelCache(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body updateCacheBody
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		model, err := svc.UpdateCacheSettings(c.Context(), claims.TenantID, c.Params("id"), body.CacheDisabled)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(model)
	}
}

type updatePricingBody struct {
	PricePerMInput  *float64 `json:"price_per_m_input"`
	PricePerMOutput *float64 `json:"price_per_m_output"`
}

func updateModelPricing(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		modelID := c.Params("id")
		var body updatePricingBody
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.PricePerMInput == nil || body.PricePerMOutput == nil {
			return c.Status(400).JSON(fiber.Map{"error": "price_per_m_input and price_per_m_output are required"})
		}
		model, err := svc.UpdatePricing(c.Context(), claims.TenantID, modelID, *body.PricePerMInput, *body.PricePerMOutput)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(model)
	}
}
