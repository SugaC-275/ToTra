package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yourorg/totra/admin/services"
)

// ---------------------------------------------------------------------------
// Service interfaces
// ---------------------------------------------------------------------------

// SIEMConfigServiceIface is the interface satisfied by services.SIEMConfigService.
type SIEMConfigServiceIface interface {
	Create(ctx context.Context, tenantID, name, endpointURL, apiKey string, eventTypes []string) (*services.SIEMConfig, error)
	List(ctx context.Context, tenantID string) ([]*services.SIEMConfig, error)
	Delete(ctx context.Context, tenantID, id string) error
}

// SIEMDeliveryServiceIface is the interface satisfied by services.SIEMDeliveryService.
type SIEMDeliveryServiceIface interface {
	SendTest(ctx context.Context, tenantID, configID string) error
	GetDeliveryLog(ctx context.Context, tenantID string, limit int) ([]*services.DeliveryLogRow, error)
}

// SIEMPullServiceIface is the interface satisfied by services.SIEMPullService.
type SIEMPullServiceIface interface {
	GetEvents(ctx context.Context, tenantID string, since time.Time, types []string, limit int) (*services.SIEMEventsResult, error)
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// RegisterSIEMRoutes registers the 6 SIEM admin endpoints onto app.
func RegisterSIEMRoutes(app fiber.Router, cfgSvc SIEMConfigServiceIface, delivSvc SIEMDeliveryServiceIface, pullSvc SIEMPullServiceIface) {
	g := app.Group("/api/admin/siem")
	g.Get("/configs", listSIEMConfigs(cfgSvc))
	g.Post("/configs", createSIEMConfig(cfgSvc))
	g.Delete("/configs/:id", deleteSIEMConfig(cfgSvc))
	g.Post("/configs/:id/test", sendSIEMTest(delivSvc))
	g.Get("/events", getSIEMEvents(pullSvc))
	g.Get("/delivery-log", getSIEMDeliveryLog(delivSvc))
}

// ---------------------------------------------------------------------------
// adminOnly helper
// ---------------------------------------------------------------------------

// adminOnly reads claims from locals and checks that role == "admin".
// Returns the claims and true on success, or writes a 403 and returns nil, false.
func adminOnly(c *fiber.Ctx) (*services.Claims, bool) {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims == nil || claims.Role != "admin" {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin access required"}) //nolint:errcheck
		return nil, false
	}
	return claims, true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func listSIEMConfigs(svc SIEMConfigServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		configs, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(configs), "configs": configs})
	}
}

func createSIEMConfig(svc SIEMConfigServiceIface) fiber.Handler {
	type request struct {
		Name        string   `json:"name"`
		EndpointURL string   `json:"endpoint_url"`
		APIKey      string   `json:"api_key"`
		EventTypes  []string `json:"event_types"`
	}
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		var req request
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Name == "" || req.EndpointURL == "" || req.APIKey == "" || len(req.EventTypes) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "name, endpoint_url, api_key, event_types are required"})
		}
		cfg, err := svc.Create(c.Context(), claims.TenantID, req.Name, req.EndpointURL, req.APIKey, req.EventTypes)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(cfg)
	}
}

func deleteSIEMConfig(svc SIEMConfigServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		id := c.Params("id")
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"deleted": id})
	}
}

func sendSIEMTest(svc SIEMDeliveryServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		id := c.Params("id")
		if err := svc.SendTest(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	}
}

func getSIEMEvents(svc SIEMPullServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}

		// since: default 24 hours ago
		since := time.Now().UTC().Add(-24 * time.Hour)
		if s := c.Query("since"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				since = t
			}
		}

		// types: comma-separated query param
		var types []string
		if t := c.Query("types"); t != "" {
			types = strings.Split(t, ",")
		}

		// limit: default 100
		limit := 100
		if l := c.Query("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		result, err := svc.GetEvents(c.Context(), claims.TenantID, since, types, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}

func getSIEMDeliveryLog(svc SIEMDeliveryServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}

		limit := 50
		if l := c.Query("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		logs, err := svc.GetDeliveryLog(c.Context(), claims.TenantID, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(logs), "log": logs})
	}
}
