package handlers

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// ABRouteStore is the storage interface required by the A/B routes handlers.
type ABRouteStore interface {
	List(ctx context.Context, tenantID string) ([]*storage.ABRouteRow, error)
	Create(ctx context.Context, tenantID, name, modelA, modelB string, pctB int, evalSuite *string) (*storage.ABRouteRow, error)
	Update(ctx context.Context, tenantID, id string, pctB int, evalSuite *string, isActive bool) (*storage.ABRouteRow, error)
	Delete(ctx context.Context, tenantID, id string) error
}

// RegisterABRouteRoutes mounts A/B route management endpoints.
//
//	GET    /admin/ab-routes        — list all routes for the tenant
//	POST   /admin/ab-routes        — create a new route
//	PUT    /admin/ab-routes/:id    — update an existing route
//	DELETE /admin/ab-routes/:id    — delete a route
func RegisterABRouteRoutes(router fiber.Router, store ABRouteStore) {
	router.Get("/admin/ab-routes", listABRoutes(store))
	router.Post("/admin/ab-routes", createABRoute(store))
	router.Put("/admin/ab-routes/:id", updateABRoute(store))
	router.Delete("/admin/ab-routes/:id", deleteABRoute(store))
}

func listABRoutes(store ABRouteStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		routes, err := store.List(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if routes == nil {
			routes = []*storage.ABRouteRow{}
		}
		return c.JSON(routes)
	}
}

type createABRouteRequest struct {
	Name      string  `json:"name"`
	ModelA    string  `json:"model_a"`
	ModelB    string  `json:"model_b"`
	PctB      int     `json:"pct_b"`
	EvalSuite *string `json:"eval_suite,omitempty"`
}

func createABRoute(store ABRouteStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		var req createABRouteRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Name == "" || req.ModelA == "" || req.ModelB == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "name, model_a, and model_b are required",
			})
		}
		if req.PctB < 1 || req.PctB > 99 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "pct_b must be between 1 and 99",
			})
		}
		route, err := store.Create(c.Context(), user.TenantID, req.Name, req.ModelA, req.ModelB, req.PctB, req.EvalSuite)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(route)
	}
}

type updateABRouteRequest struct {
	PctB      int     `json:"pct_b"`
	EvalSuite *string `json:"eval_suite,omitempty"`
	IsActive  bool    `json:"is_active"`
}

func updateABRoute(store ABRouteStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		id := c.Params("id")
		var req updateABRouteRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.PctB < 1 || req.PctB > 99 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "pct_b must be between 1 and 99",
			})
		}
		route, err := store.Update(c.Context(), user.TenantID, id, req.PctB, req.EvalSuite, req.IsActive)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if route == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "route not found"})
		}
		return c.JSON(route)
	}
}

func deleteABRoute(store ABRouteStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		id := c.Params("id")
		if err := store.Delete(c.Context(), user.TenantID, id); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
