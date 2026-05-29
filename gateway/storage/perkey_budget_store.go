package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserBudget struct {
	BudgetUSD    *float64
	BudgetPeriod string
	RPMLimit     *int
	TPMLimit     *int
}

type PerkeyBudgetStore struct{ pool *pgxpool.Pool }

func NewPerkeyBudgetStore(pool *pgxpool.Pool) *PerkeyBudgetStore {
	return &PerkeyBudgetStore{pool: pool}
}

func (s *PerkeyBudgetStore) GetBudget(ctx context.Context, userID string) (*float64, string, error) {
	var b UserBudget
	err := s.pool.QueryRow(ctx,
		`SELECT budget_usd, COALESCE(budget_period,'monthly'), rpm_limit, tpm_limit
		 FROM users WHERE id=$1`,
		userID,
	).Scan(&b.BudgetUSD, &b.BudgetPeriod, &b.RPMLimit, &b.TPMLimit)
	if err != nil {
		return nil, "", fmt.Errorf("perkey_budget get: %w", err)
	}
	return b.BudgetUSD, b.BudgetPeriod, nil
}

func (s *PerkeyBudgetStore) GetSpent(ctx context.Context, userID, periodKey string) (float64, error) {
	var spent float64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(usd_spent, 0) FROM budget_consumption_log WHERE user_id=$1 AND period_key=$2`,
		userID, periodKey,
	).Scan(&spent)
	if err != nil {
		return 0, nil
	}
	return spent, nil
}

func (s *PerkeyBudgetStore) IncrementSpend(ctx context.Context, userID, tenantID, periodKey string, usdAmount float64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO budget_consumption_log (user_id, tenant_id, usd_spent, period_key)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, period_key) DO UPDATE
		 SET usd_spent = budget_consumption_log.usd_spent + EXCLUDED.usd_spent,
		     updated_at = NOW()`,
		userID, tenantID, usdAmount, periodKey,
	)
	return err
}

func (s *PerkeyBudgetStore) SetBudget(ctx context.Context, userID string, budgetUSD *float64, period string, rpmLimit, tpmLimit *int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET budget_usd=$1, budget_period=COALESCE(NULLIF($2,''),'monthly'), rpm_limit=$3, tpm_limit=$4 WHERE id=$5`,
		budgetUSD, period, rpmLimit, tpmLimit, userID,
	)
	return err
}

// BudgetPeriodKey returns the current period key for the given period type.
func BudgetPeriodKey(period string) string {
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
