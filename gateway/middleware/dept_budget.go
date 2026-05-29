package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// DeptBudgetQuerier is satisfied by storage.DepartmentStore.
type DeptBudgetQuerier interface {
	GetDeptBudget(ctx context.Context, deptID string) (*float64, error)
	GetSpend(ctx context.Context, deptID, periodKey string) (float64, error)
}

// NewDeptBudgetMiddleware enforces department-level USD spending caps.
// It runs after the per-key budget middleware and short-circuits with 429
// when the department's budget is exhausted.
func NewDeptBudgetMiddleware(store DeptBudgetQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil || user.DepartmentID == "" {
			return c.Next()
		}

		budgetUSD, err := store.GetDeptBudget(c.Context(), user.DepartmentID)
		if err != nil {
			slog.Warn("dept_budget: get budget", "dept", user.DepartmentID, "err", err)
			return c.Next()
		}
		if budgetUSD == nil {
			return c.Next()
		}

		periodKey := deptBudgetPeriodKey()
		spent, err := store.GetSpend(c.Context(), user.DepartmentID, periodKey)
		if err != nil {
			slog.Warn("dept_budget: get spend", "dept", user.DepartmentID, "err", err)
			return c.Next()
		}

		if spent >= *budgetUSD {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "department budget exceeded",
					"type":    "budget_exceeded",
					"code":    "dept_budget_exceeded",
				},
			})
		}

		return c.Next()
	}
}

// deptBudgetPeriodKey returns the monthly period key (department budgets are monthly by default).
func deptBudgetPeriodKey() string {
	return time.Now().UTC().Format("2006-01")
}
