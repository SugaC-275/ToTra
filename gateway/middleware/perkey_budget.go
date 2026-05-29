package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

type BudgetQuerier interface {
	GetBudget(ctx context.Context, userID string) (budgetUSD *float64, period string, err error)
	GetSpent(ctx context.Context, userID, periodKey string) (float64, error)
	IncrementSpend(ctx context.Context, userID, tenantID, periodKey string, amount float64) error
}

// NewPerkeyBudgetMiddleware enforces per-key USD spending caps.
// Checks the limit pre-request and increments spend post-request from response token counts.
func NewPerkeyBudgetMiddleware(store BudgetQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}

		budgetUSD, period, err := store.GetBudget(c.Context(), user.UserID)
		if err != nil {
			slog.Warn("perkey_budget: get budget", "user", user.UserID, "err", err)
			return c.Next()
		}
		if budgetUSD == nil {
			return c.Next()
		}

		periodKey := keyBudgetPeriod(period)
		spent, err := store.GetSpent(c.Context(), user.UserID, periodKey)
		if err != nil {
			slog.Warn("perkey_budget: get spent", "user", user.UserID, "err", err)
			return c.Next()
		}

		if spent >= *budgetUSD {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "key budget exceeded",
					"type":    "budget_exceeded",
					"code":    "budget_exceeded",
				},
			})
		}

		if err := c.Next(); err != nil {
			return err
		}

		var resp struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if jsonErr := json.Unmarshal(c.Response().Body(), &resp); jsonErr == nil {
			total := resp.Usage.PromptTokens + resp.Usage.CompletionTokens
			if total > 0 {
				// $0.01/1K tokens is a conservative default; actual cost depends on model pricing.
				estimated := float64(total) / 1000.0 * 0.01
				tenantID := user.TenantID
				userID := user.UserID
				go func() {
					_ = store.IncrementSpend(context.Background(), userID, tenantID, periodKey, estimated)
				}()
			}
		}
		return nil
	}
}

func keyBudgetPeriod(period string) string {
	now := time.Now().UTC()
	switch period {
	case "daily":
		return now.Format("2006-01-02")
	case "total":
		return "total"
	default:
		return now.Format("2006-01")
	}
}
