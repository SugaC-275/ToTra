package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DeptMonthlyBudget struct {
	Department string  `json:"department"`
	MonthlyUSD float64 `json:"monthly_usd"`
	AnnualUSD  float64 `json:"annual_usd"`
	GrowthRate float64 `json:"growth_rate_percent"` // month-over-month annualised
}

type QuarterlyBudget struct {
	Quarter string  `json:"quarter"` // "2026-Q3"
	USD     float64 `json:"usd"`
}

type BudgetPlan struct {
	TenantID        string              `json:"tenant_id"`
	PlanYear        int                 `json:"plan_year"`
	TotalAnnualUSD  float64             `json:"total_annual_usd"`
	ByDepartment    []DeptMonthlyBudget `json:"by_department"`
	Quarterly       []QuarterlyBudget   `json:"quarterly"`
	GrowthRateUsed  float64             `json:"growth_rate_used_percent"`
	GeneratedAt     string              `json:"generated_at"`
}

// ProjectedMonthlySpend returns spend after applying a monthly growth rate for n months.
func ProjectedMonthlySpend(baseUSD, monthlyGrowthRate float64, n int) float64 {
	if baseUSD <= 0 {
		return 0
	}
	return baseUSD * pow(1+monthlyGrowthRate/100, float64(n))
}

func pow(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// AnnualisedGrowthRate converts a slope from LinearRegression(monthly totals) to a
// percentage monthly growth rate relative to the mean.
func AnnualisedGrowthRate(slope, mean float64) float64 {
	if mean == 0 {
		return 0
	}
	return slope / mean * 100
}

type BudgetPlannerService struct{ pool *pgxpool.Pool }

func NewBudgetPlannerService(pool *pgxpool.Pool) *BudgetPlannerService {
	return &BudgetPlannerService{pool: pool}
}

func (s *BudgetPlannerService) GetBudgetPlan(ctx context.Context, tenantID string, planYear int) (*BudgetPlan, error) {
	if planYear == 0 {
		planYear = time.Now().UTC().Year() + 1
	}

	plan := &BudgetPlan{
		TenantID:    tenantID,
		PlanYear:    planYear,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Compute monthly growth rate from last 6 months of overall spend.
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND request_at >= NOW() - INTERVAL '6 months'
		GROUP BY to_char(request_at AT TIME ZONE 'UTC','YYYY-MM')
		ORDER BY to_char(request_at AT TIME ZONE 'UTC','YYYY-MM') ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("budget plan history: %w", err)
	}
	defer rows.Close()
	var monthly []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("budget plan history scan: %w", err)
		}
		monthly = append(monthly, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("budget plan history rows: %w", err)
	}

	slope, _ := LinearRegression(monthly)
	var mean float64
	for _, v := range monthly {
		mean += v
	}
	if len(monthly) > 0 {
		mean /= float64(len(monthly))
	}
	growthRate := AnnualisedGrowthRate(slope, mean)
	plan.GrowthRateUsed = growthRate

	// Project 12 months for planYear starting from next month.
	monthsUntilPlanYear := 0
	now := time.Now().UTC()
	for now.AddDate(0, monthsUntilPlanYear, 0).Year() < planYear {
		monthsUntilPlanYear++
	}

	var totalAnnual float64
	quarterly := map[string]float64{}
	for m := 0; m < 12; m++ {
		t := time.Date(planYear, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)
		projected := ProjectedMonthlySpend(mean, growthRate, monthsUntilPlanYear+m)
		totalAnnual += projected
		q := fmt.Sprintf("%d-Q%d", planYear, (m/3)+1)
		quarterly[q] += projected
		_ = t
	}
	plan.TotalAnnualUSD = totalAnnual

	for _, qname := range []string{
		fmt.Sprintf("%d-Q1", planYear),
		fmt.Sprintf("%d-Q2", planYear),
		fmt.Sprintf("%d-Q3", planYear),
		fmt.Sprintf("%d-Q4", planYear),
	} {
		plan.Quarterly = append(plan.Quarterly, QuarterlyBudget{Quarter: qname, USD: quarterly[qname]})
	}

	// Per-department budget: scale each dept's share of last-month spend by projected total/12.
	drows, err := s.pool.Query(ctx, `
		SELECT COALESCE(u.department, 'Other'), COALESCE(SUM(r.usd_cost), 0)
		FROM usage_records r
		LEFT JOIN users u ON u.id = r.user_id
		WHERE r.tenant_id = $1
		  AND to_char(r.request_at AT TIME ZONE 'UTC','YYYY-MM') = $2
		GROUP BY u.department
		ORDER BY SUM(r.usd_cost) DESC
	`, tenantID, now.AddDate(0, -1, 0).Format("2006-01"))
	if err != nil {
		return nil, fmt.Errorf("budget plan dept: %w", err)
	}
	defer drows.Close()

	var depts []struct {
		dept string
		usd  float64
	}
	var deptTotal float64
	for drows.Next() {
		var dept string
		var usd float64
		if err := drows.Scan(&dept, &usd); err != nil {
			return nil, fmt.Errorf("budget plan dept scan: %w", err)
		}
		depts = append(depts, struct {
			dept string
			usd  float64
		}{dept, usd})
		deptTotal += usd
	}
	if err := drows.Err(); err != nil {
		return nil, fmt.Errorf("budget plan dept rows: %w", err)
	}

	projectedMonthly := totalAnnual / 12
	for _, d := range depts {
		share := 0.0
		if deptTotal > 0 {
			share = d.usd / deptTotal
		}
		monthly := projectedMonthly * share
		plan.ByDepartment = append(plan.ByDepartment, DeptMonthlyBudget{
			Department: d.dept,
			MonthlyUSD: monthly,
			AnnualUSD:  monthly * 12,
			GrowthRate: growthRate,
		})
	}

	if plan.ByDepartment == nil {
		plan.ByDepartment = []DeptMonthlyBudget{}
	}
	return plan, nil
}
