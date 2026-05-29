package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

type BudgetStore interface {
	GetBudget(ctx context.Context, userID string) (budgetUSD *float64, period string, err error)
	GetSpent(ctx context.Context, userID, periodKey string) (float64, error)
	SetBudget(ctx context.Context, userID string, budgetUSD *float64, period string, rpmLimit, tpmLimit *int) error
}

func RegisterBudgetRoutes(router fiber.Router, store BudgetStore) {
	router.Get("/key/budget", getKeyBudget(store))
	router.Put("/key/budget", setKeyBudget(store))
}

func getKeyBudget(store BudgetStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		budgetUSD, period, err := store.GetBudget(c.Context(), user.UserID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		spent, _ := store.GetSpent(c.Context(), user.UserID, keyBudgetPeriodKey(period))
		return c.JSON(fiber.Map{
			"user_id":    user.UserID,
			"budget_usd": budgetUSD,
			"period":     period,
			"spent_usd":  spent,
		})
	}
}

type setBudgetPayload struct {
	BudgetUSD *float64 `json:"budget_usd"`
	Period    string   `json:"period"`
	RPMLimit  *int     `json:"rpm_limit"`
	TPMLimit  *int     `json:"tpm_limit"`
}

func setKeyBudget(store BudgetStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		var payload setBudgetPayload
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if err := store.SetBudget(c.Context(), user.UserID, payload.BudgetUSD, payload.Period, payload.RPMLimit, payload.TPMLimit); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func keyBudgetPeriodKey(period string) string {
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
