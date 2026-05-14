package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ValidateWorkHours(start, end int) error {
	if start < 0 || start > 23 || end < 0 || end > 23 {
		return fmt.Errorf("work hours must be in range 0-23")
	}
	if end <= start {
		return fmt.Errorf("work_end_hour must be greater than work_start_hour")
	}
	return nil
}

type CostSettings struct {
	TenantID         string   `json:"tenant_id"`
	MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
	WorkStartHour    int      `json:"work_start_hour"`
	WorkEndHour      int      `json:"work_end_hour"`
	UpdatedAt        string   `json:"updated_at"`
}

type CostSettingsService struct{ pool *pgxpool.Pool }

func NewCostSettingsService(pool *pgxpool.Pool) *CostSettingsService {
	return &CostSettingsService{pool: pool}
}

func (s *CostSettingsService) Get(ctx context.Context, tenantID string) (*CostSettings, error) {
	cs := &CostSettings{TenantID: tenantID, WorkStartHour: 9, WorkEndHour: 18}
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT monthly_budget_usd, work_start_hour, work_end_hour, updated_at
		FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&cs.MonthlyBudgetUSD, &cs.WorkStartHour, &cs.WorkEndHour, &updatedAt)
	if err != nil {
		cs.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return cs, nil
	}
	cs.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return cs, nil
}

func (s *CostSettingsService) Upsert(ctx context.Context, tenantID string, monthlyBudgetUSD *float64, workStartHour, workEndHour int) (*CostSettings, error) {
	if err := ValidateWorkHours(workStartHour, workEndHour); err != nil {
		return nil, err
	}
	cs := &CostSettings{TenantID: tenantID, MonthlyBudgetUSD: monthlyBudgetUSD, WorkStartHour: workStartHour, WorkEndHour: workEndHour}
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_cost_settings(tenant_id, monthly_budget_usd, work_start_hour, work_end_hour)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE
		  SET monthly_budget_usd=$2, work_start_hour=$3, work_end_hour=$4, updated_at=NOW()
		RETURNING monthly_budget_usd, work_start_hour, work_end_hour, updated_at
	`, tenantID, monthlyBudgetUSD, workStartHour, workEndHour).Scan(
		&cs.MonthlyBudgetUSD, &cs.WorkStartHour, &cs.WorkEndHour, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("cost settings upsert: %w", err)
	}
	cs.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return cs, nil
}
