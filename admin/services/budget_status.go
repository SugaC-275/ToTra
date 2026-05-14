package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func BudgetPercentage(spent, budget float64) float64 {
	if budget == 0 {
		return 0
	}
	return spent / budget * 100
}

type BudgetStatus struct {
	TenantID         string   `json:"tenant_id"`
	YearMonth        string   `json:"year_month"`
	SpentUSD         float64  `json:"spent_usd"`
	MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
	UsedPercent      float64  `json:"used_percent"`
	IsOverBudget     bool     `json:"is_over_budget"`
	GeneratedAt      string   `json:"generated_at"`
}

type BudgetStatusService struct{ pool *pgxpool.Pool }

func NewBudgetStatusService(pool *pgxpool.Pool) *BudgetStatusService {
	return &BudgetStatusService{pool: pool}
}

func (s *BudgetStatusService) GetBudgetStatus(ctx context.Context, tenantID string) (*BudgetStatus, error) {
	yearMonth := time.Now().UTC().Format("2006-01")
	var spentUSD float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&spentUSD)
	if err != nil {
		return nil, fmt.Errorf("budget status spend query: %w", err)
	}
	var budgetUSD *float64
	_ = s.pool.QueryRow(ctx, `
		SELECT monthly_budget_usd FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&budgetUSD)

	usedPercent := 0.0
	isOver := false
	if budgetUSD != nil {
		usedPercent = BudgetPercentage(spentUSD, *budgetUSD)
		isOver = spentUSD > *budgetUSD
	}
	return &BudgetStatus{
		TenantID:         tenantID,
		YearMonth:        yearMonth,
		SpentUSD:         spentUSD,
		MonthlyBudgetUSD: budgetUSD,
		UsedPercent:      usedPercent,
		IsOverBudget:     isOver,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}
