package middleware

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// RateLimitChecker is the interface the middleware depends on, allowing mocks
// in tests without importing the storage package.
type RateLimitChecker interface {
	GetConfig(ctx context.Context, tenantID string) (maxPerMin, maxPerUserPerMin int, err error)
	CheckAndIncrement(ctx context.Context, tenantID, userID string, limit int) (allowed bool, remaining, retryAfterSeconds int, err error)
}

// NewRateLimiterMiddleware returns a Fiber handler that enforces per-tenant and
// per-user request-per-minute limits using a Redis sliding window.
//
// It reads the authenticated user from c.Locals("user") set by NewAuthMiddleware.
// Unauthenticated requests are passed through (auth middleware handles rejection).
//
// Two checks are performed in order:
//  1. Tenant-wide limit  (maxPerMin from rate_limit_configs)
//  2. Per-user limit     (maxPerUserPerMin from rate_limit_configs)
//
// On denial: HTTP 429 with JSON body and Retry-After header.
func NewRateLimiterMiddleware(store RateLimitChecker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok {
			return c.Next()
		}

		tenantLimit, userLimit, err := store.GetConfig(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "rate limit config unavailable", "type": "server_error"},
			})
		}

		// --- tenant-level check ---
		// Use a synthetic "all-users" bucket for the tenant aggregate.
		tenantAllowed, tenantRemaining, retryAfter, err := store.CheckAndIncrement(
			c.Context(), user.TenantID, "_tenant", tenantLimit,
		)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "rate limit check failed", "type": "server_error"},
			})
		}
		if !tenantAllowed {
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message":     "rate limit exceeded",
					"retry_after": retryAfter,
				},
			})
		}

		// --- per-user check ---
		userAllowed, userRemaining, retryAfter, err := store.CheckAndIncrement(
			c.Context(), user.TenantID, user.UserID, userLimit,
		)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "rate limit check failed", "type": "server_error"},
			})
		}
		if !userAllowed {
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message":     "rate limit exceeded",
					"retry_after": retryAfter,
				},
			})
		}

		// Expose the more-constraining remaining count to callers.
		remaining := tenantRemaining
		if userRemaining < remaining {
			remaining = userRemaining
		}
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", userLimit))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		return c.Next()
	}
}
