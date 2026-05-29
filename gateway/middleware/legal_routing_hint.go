package middleware

import "github.com/gofiber/fiber/v2"

// IsPrivilegedRequest returns true if the current request has been flagged
// as potentially privileged by NewLegalPrivilegeMiddleware.
// Callers (proxy handler) use this to enforce attorney_privilege_routing.
func IsPrivilegedRequest(c *fiber.Ctx) bool {
	v, _ := c.Locals("privilege_detected").(bool)
	return v
}

// PrivilegeRoutingError returns the standard 451 error body for privileged
// requests rejected due to non-compliant model routing.
func PrivilegeRoutingError() fiber.Map {
	return fiber.Map{
		"error": fiber.Map{
			"message": "Privileged communication detected: request must be routed to an attorney-privilege-routing-enabled model",
			"type":    "legal_compliance_error",
			"code":    "privilege_routing_required",
		},
	}
}
