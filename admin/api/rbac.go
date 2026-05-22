package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// RegisterRBACRoutes registers role management endpoints. All routes require admin.
func RegisterRBACRoutes(app fiber.Router, svc services.RBACServiceIface) {
	app.Get("/api/admin/users/:id/roles", listUserRoles(svc))
	app.Post("/api/admin/users/:id/roles", assignUserRole(svc))
	app.Delete("/api/admin/users/:id/roles/:roleId", revokeUserRole(svc))
}

func listUserRoles(svc services.RBACServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		userID := c.Params("id")
		if userID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user id required"})
		}
		roles, err := svc.GetUserRoles(c.Context(), claims.TenantID, userID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"user_id": userID, "roles": roles})
	}
}

type assignRoleRequest struct {
	Role       string `json:"role"`
	Department string `json:"department"`
}

func assignUserRole(svc services.RBACServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		userID := c.Params("id")
		if userID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user id required"})
		}
		var req assignRoleRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Role == "" {
			return c.Status(400).JSON(fiber.Map{"error": "role is required"})
		}
		role := services.Role(req.Role)
		switch role {
		case services.RoleAdmin, services.RoleDeptAdmin, services.RoleComplianceOfficer, services.RoleAuditor, services.RoleUser:
			// valid
		default:
			return c.Status(400).JSON(fiber.Map{"error": "invalid role: " + req.Role})
		}
		if err := svc.AssignRole(c.Context(), claims.TenantID, userID, role, req.Department, claims.UserID); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"status": "assigned", "user_id": userID, "role": req.Role})
	}
}

// revokeUserRole deletes a role by looking up the user's current roles and revoking by roleId (the UUID row id).
// The client passes the UUID from GET /roles; we look it up to determine role+department for the delete.
func revokeUserRole(svc services.RBACServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		userID := c.Params("id")
		roleID := c.Params("roleId")
		if userID == "" || roleID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user id and role id required"})
		}
		// Fetch current roles to find the matching entry.
		roles, err := svc.GetUserRoles(c.Context(), claims.TenantID, userID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		var found *services.UserRole
		for i := range roles {
			if roles[i].ID == roleID {
				found = &roles[i]
				break
			}
		}
		if found == nil {
			return c.Status(404).JSON(fiber.Map{"error": "role assignment not found"})
		}
		if err := svc.RevokeRole(c.Context(), claims.TenantID, userID, found.Role, found.Department); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "revoked"})
	}
}
