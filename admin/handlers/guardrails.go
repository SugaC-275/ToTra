package handlers

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// GuardrailStore is the storage interface required by the guardrail handlers.
// It uses services.GuardrailConfig so the admin package does not depend on
// the gateway/storage package.
type GuardrailStore interface {
	ListGuardrailConfigs(ctx context.Context, tenantID string) ([]*services.GuardrailConfig, error)
	UpsertGuardrailConfig(ctx context.Context, tenantID, name string, enabled bool, strictness string, customConfig map[string]any, bundleID *string) (*services.GuardrailConfig, error)
	DeleteGuardrailConfig(ctx context.Context, tenantID, name string) error
}

// defaultGuardrailNames lists the known guardrail check names.
var defaultGuardrailNames = []string{
	"pii_scan",
	"injection_detect",
	"policy_rules",
	"response_pii",
}

// RegisterGuardrailRoutes mounts guardrail configuration endpoints.
//
//	GET    /api/guardrails            — list all guardrail configs for tenant
//	PUT    /api/guardrails/:name      — update a guardrail config
//	POST   /api/guardrails/reset      — reset to bundle defaults (delete custom overrides)
func RegisterGuardrailRoutes(r fiber.Router, store GuardrailStore) {
	r.Get("/api/guardrails", listGuardrails(store))
	r.Put("/api/guardrails/:name", updateGuardrail(store))
	r.Post("/api/guardrails/reset", resetGuardrails(store))
}

func listGuardrails(store GuardrailStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		configs, err := store.ListGuardrailConfigs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if configs == nil {
			configs = []*services.GuardrailConfig{}
		}
		return c.JSON(configs)
	}
}

type updateGuardrailRequest struct {
	Enabled      bool           `json:"enabled"`
	Strictness   string         `json:"strictness"`
	CustomConfig map[string]any `json:"custom_config"`
	BundleID     *string        `json:"bundle_id,omitempty"`
}

func updateGuardrail(store GuardrailStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		name := c.Params("name")
		var req updateGuardrailRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Strictness == "" {
			req.Strictness = "standard"
		}
		validStrictness := map[string]bool{"permissive": true, "standard": true, "strict": true}
		if !validStrictness[req.Strictness] {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "strictness must be permissive, standard, or strict",
			})
		}
		cfg, err := store.UpsertGuardrailConfig(c.Context(), claims.TenantID, name,
			req.Enabled, req.Strictness, req.CustomConfig, req.BundleID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(cfg)
	}
}

func resetGuardrails(store GuardrailStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		for _, name := range defaultGuardrailNames {
			if err := store.DeleteGuardrailConfig(c.Context(), claims.TenantID, name); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
			}
		}
		return c.JSON(fiber.Map{"status": "reset", "guardrails": defaultGuardrailNames})
	}
}
