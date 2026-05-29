package handlers

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// FailoverModelLookup is the storage interface required by the failover API handlers.
type FailoverModelLookup interface {
	// GetByName resolves a model config by tenant + name. Returns (nil, nil) when not found.
	GetByName(ctx context.Context, tenantID, name string) (*storage.ModelConfig, error)
	// GetFallbackChain returns the ordered fallback chain for a primary model.
	GetFallbackChain(ctx context.Context, tenantID, primaryModelName string) ([]*middleware.ModelConfig, error)
	// SetFallback links modelConfigID → fallbackModelConfigID in the DB.
	SetFallback(ctx context.Context, modelConfigID, fallbackModelConfigID string) error
	// ClearFallback sets fallback_model_config_id = NULL for the given model.
	ClearFallback(ctx context.Context, modelConfigID string) error
}

// RegisterFailoverRoutes mounts the failover configuration endpoints:
//
//	GET    /v1/failover/:model_name  — return the current fallback chain
//	PUT    /v1/failover/:model_name  — set a new fallback chain
//	DELETE /v1/failover/:model_name  — clear all fallbacks for the model
func RegisterFailoverRoutes(router fiber.Router, lookup FailoverModelLookup) {
	router.Get("/failover/:model_name", getFailoverChain(lookup))
	router.Put("/failover/:model_name", setFailoverChain(lookup))
	router.Delete("/failover/:model_name", clearFailoverChain(lookup))
}

// getFailoverChain returns the current fallback chain for a model.
//
// Response: { "model": "gpt-4o", "chain": ["gpt-4o-azure", "claude-3-5-sonnet"] }
func getFailoverChain(lookup FailoverModelLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		modelName := c.Params("model_name")
		chain, err := lookup.GetFallbackChain(c.Context(), user.TenantID, modelName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		names := make([]string, 0, len(chain))
		for _, m := range chain {
			names = append(names, m.Name)
		}

		return c.JSON(fiber.Map{
			"model": modelName,
			"chain": names,
		})
	}
}

type setChainPayload struct {
	FallbackModels []string `json:"fallback_models"`
}

// setFailoverChain replaces the fallback chain for a model.
//
// Body: { "fallback_models": ["gpt-4o-azure", "claude-3-5-sonnet"] }
//
// The handler:
//  1. Validates every name in fallback_models exists for this tenant.
//  2. Links them in order: primary → fb[0] → fb[1] → ... → fb[n] → NULL.
func setFailoverChain(lookup FailoverModelLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		modelName := c.Params("model_name")

		var payload setChainPayload
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}

		// Resolve primary model.
		primary, err := lookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if primary == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "model not found"})
		}

		// Resolve and validate each fallback model.
		type idName struct{ id, name string }
		resolved := make([]idName, 0, len(payload.FallbackModels))
		for _, name := range payload.FallbackModels {
			mc, err := lookup.GetByName(c.Context(), user.TenantID, name)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
			}
			if mc == nil {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": "fallback model not found: " + name,
				})
			}
			resolved = append(resolved, idName{id: mc.ID, name: mc.Name})
		}

		// Build the linked list: primary → fb[0] → fb[1] → ... → NULL.
		// Collect IDs in order for easy iteration.
		allIDs := make([]string, 0, 1+len(resolved))
		allIDs = append(allIDs, primary.ID)
		for _, r := range resolved {
			allIDs = append(allIDs, r.id)
		}

		for i := 0; i < len(allIDs)-1; i++ {
			if err := lookup.SetFallback(c.Context(), allIDs[i], allIDs[i+1]); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
			}
		}
		// Clear the last node's fallback pointer.
		if err := lookup.ClearFallback(c.Context(), allIDs[len(allIDs)-1]); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		names := make([]string, 0, len(resolved))
		for _, r := range resolved {
			names = append(names, r.name)
		}

		return c.JSON(fiber.Map{
			"model": modelName,
			"chain": names,
		})
	}
}

// clearFailoverChain removes all fallback links for a model (sets its fallback to NULL).
func clearFailoverChain(lookup FailoverModelLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		modelName := c.Params("model_name")

		primary, err := lookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if primary == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "model not found"})
		}

		if err := lookup.ClearFallback(c.Context(), primary.ID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		return c.JSON(fiber.Map{
			"model": modelName,
			"chain": []string{},
		})
	}
}
