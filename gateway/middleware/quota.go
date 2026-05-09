package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

type QuotaChecker interface {
	CheckAndIncrement(ctx context.Context, tenantID, userID, yearMonth string, quotaLimit, cost int) (bool, int, error)
}

type UserQuotaFetcher interface {
	GetUserQuota(ctx context.Context, tenantID, userID string) (int, error)
}

func NewQuotaMiddleware(qs QuotaChecker, uq UserQuotaFetcher) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok {
			return c.Next()
		}

		yearMonth := time.Now().Format("2006-01")
		quotaLimit, err := uq.GetUserQuota(c.Context(), user.TenantID, user.UserID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "quota lookup failed", "type": "server_error"},
			})
		}

		estimatedCost := 100
		allowed, remaining, err := qs.CheckAndIncrement(c.Context(), user.TenantID, user.UserID, yearMonth, quotaLimit, estimatedCost)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "quota check failed", "type": "server_error"},
			})
		}

		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", quotaLimit))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if !allowed {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("quota exceeded. limit: %d SCU/month", quotaLimit),
					"type":    "quota_exceeded",
					"code":    "quota_exceeded",
				},
			})
		}

		return c.Next()
	}
}
