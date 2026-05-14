package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type BotBroadcaster interface {
	BroadcastAlert(ctx context.Context, tenantID, message string) error
}

type BudgetAlertService struct{ pool *pgxpool.Pool }

func NewBudgetAlertService(pool *pgxpool.Pool) *BudgetAlertService {
	return &BudgetAlertService{pool: pool}
}

// CheckAndNotify checks the current month budget usage and sends alerts for each
// newly crossed threshold (50%, 80%, 100%). DB dedup prevents duplicate sends.
func (s *BudgetAlertService) CheckAndNotify(ctx context.Context, tenantID string, bot BotBroadcaster) error {
	yearMonth := time.Now().UTC().Format("2006-01")

	var spentUSD float64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&spentUSD); err != nil {
		return fmt.Errorf("budget alert: spend query: %w", err)
	}

	var budgetUSD *float64
	_ = s.pool.QueryRow(ctx, `
		SELECT monthly_budget_usd FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&budgetUSD)

	if budgetUSD == nil || *budgetUSD == 0 {
		return nil
	}

	usedPercent := BudgetPercentage(spentUSD, *budgetUSD)
	thresholds := CrossedThresholds(usedPercent)

	for _, pct := range thresholds {
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO budget_alert_log (tenant_id, year_month, threshold_pct)
			VALUES ($1, $2, $3)
			ON CONFLICT ON CONSTRAINT uq_budget_alert_log DO NOTHING
		`, tenantID, yearMonth, pct)
		if err != nil {
			return fmt.Errorf("budget alert: log insert: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue // already sent this threshold this month
		}
		msg := fmt.Sprintf("Budget alert: tenant %s has used %.1f%% of monthly budget ($%.2f / $%.2f). Threshold: %d%%.",
			tenantID, usedPercent, spentUSD, *budgetUSD, pct)
		if err := bot.BroadcastAlert(ctx, tenantID, msg); err != nil {
			return fmt.Errorf("budget alert: broadcast: %w", err)
		}
	}
	return nil
}
