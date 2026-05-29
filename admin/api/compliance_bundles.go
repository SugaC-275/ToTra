package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// ComplianceBundleServiceIface is the interface the bundles handler depends on.
type ComplianceBundleServiceIface interface {
	ListActivations(ctx context.Context, tenantID string) ([]*services.BundleActivation, error)
	GetActivation(ctx context.Context, tenantID, bundleID string) (*services.BundleActivation, error)
	Activate(ctx context.Context, tenantID, bundleID, activatedBy string) (*services.BundleActivation, error)
	Deactivate(ctx context.Context, tenantID, bundleID string) error
}

// bundleWithStatus wraps a ComplianceBundle with its activation state for the tenant.
type bundleWithStatus struct {
	services.ComplianceBundle
	IsActive    bool                     `json:"is_active"`
	Activation  *services.BundleActivation `json:"activation,omitempty"`
}

// RegisterComplianceBundleRoutes mounts compliance bundle endpoints.
//
//	GET    /api/compliance/bundles              — list all bundles with status
//	POST   /api/compliance/bundles/:id/activate — activate a bundle
//	DELETE /api/compliance/bundles/:id          — deactivate a bundle
func RegisterComplianceBundleRoutes(r fiber.Router, svc ComplianceBundleServiceIface) {
	r.Get("/api/compliance/bundles", listBundles(svc))
	r.Post("/api/compliance/bundles/:id/activate", activateBundle(svc))
	r.Delete("/api/compliance/bundles/:id", deactivateBundle(svc))
}

func listBundles(svc ComplianceBundleServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		activations, err := svc.ListActivations(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}

		// Index activations by bundle ID for O(1) lookup.
		activeMap := make(map[string]*services.BundleActivation, len(activations))
		for _, a := range activations {
			activeMap[a.BundleID] = a
		}

		// Deterministic ordering: healthcare, legal, government, finance, education.
		order := []string{"healthcare", "legal", "government", "finance", "education"}
		result := make([]bundleWithStatus, 0, len(order))
		for _, id := range order {
			bundle, exists := services.AllBundles[id]
			if !exists {
				continue
			}
			entry := bundleWithStatus{ComplianceBundle: bundle}
			if a, active := activeMap[id]; active {
				entry.IsActive = true
				entry.Activation = a
			}
			result = append(result, entry)
		}
		return c.JSON(result)
	}
}

func activateBundle(svc ComplianceBundleServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		bundleID := c.Params("id")
		if _, known := services.AllBundles[bundleID]; !known {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "unknown bundle"})
		}
		activation, err := svc.Activate(c.Context(), claims.TenantID, bundleID, claims.UserID)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"status":     "activated",
			"bundle_id":  bundleID,
			"activation": activation,
			"bundle":     services.AllBundles[bundleID],
		})
	}
}

func deactivateBundle(svc ComplianceBundleServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		bundleID := c.Params("id")
		if err := svc.Deactivate(c.Context(), claims.TenantID, bundleID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
