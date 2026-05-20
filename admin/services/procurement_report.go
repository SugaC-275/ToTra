package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProviderSpend struct {
	Provider         string  `json:"provider"`
	TotalUSD         float64 `json:"total_usd"`
	RequestCount     int64   `json:"request_count"`
	SharePercent     float64 `json:"share_percent"`
	MoMGrowthPercent float64 `json:"mom_growth_percent"` // month-over-month
}

type ModelSpend struct {
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	TotalUSD     float64 `json:"total_usd"`
	RequestCount int64   `json:"request_count"`
}

type ProcurementReport struct {
	TenantID       string          `json:"tenant_id"`
	PeriodMonths   int             `json:"period_months"`
	TotalUSD       float64         `json:"total_usd"`
	ByProvider     []ProviderSpend `json:"by_provider"`
	TopModels      []ModelSpend    `json:"top_models"`
	PeakMonthUSD   float64         `json:"peak_month_usd"`
	PeakMonth      string          `json:"peak_month"`
	AvgMonthlyUSD  float64         `json:"avg_monthly_usd"`
	ForecastedEOY  float64         `json:"forecasted_eoy_usd"`
	GeneratedAt    string          `json:"generated_at"`
}

// ProviderSharePercent returns the percentage of total spend for a provider.
func ProviderSharePercent(providerUSD, totalUSD float64) float64 {
	if totalUSD == 0 {
		return 0
	}
	return providerUSD / totalUSD * 100
}

// MoMGrowth returns the month-over-month growth percentage.
// Returns 0 when previous is 0 (no prior data).
func MoMGrowth(current, previous float64) float64 {
	if previous == 0 {
		return 0
	}
	return (current - previous) / previous * 100
}

type ProcurementReportService struct{ pool *pgxpool.Pool }

func NewProcurementReportService(pool *pgxpool.Pool) *ProcurementReportService {
	return &ProcurementReportService{pool: pool}
}

func (s *ProcurementReportService) GetProcurementReport(ctx context.Context, tenantID string, periodMonths int) (*ProcurementReport, error) {
	if periodMonths <= 0 {
		periodMonths = 6
	}
	interval := fmt.Sprintf("%d months", periodMonths)

	report := &ProcurementReport{
		TenantID:     tenantID,
		PeriodMonths: periodMonths,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	// Total spend for the period.
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(r.usd_cost), 0)
		FROM usage_records r
		WHERE r.tenant_id = $1
		  AND r.request_at >= NOW() - $2::interval
	`, tenantID, interval).Scan(&report.TotalUSD); err != nil {
		return nil, fmt.Errorf("procurement total: %w", err)
	}

	// Spend by provider (current period vs prior period for MoM on last month).
	rows, err := s.pool.Query(ctx, `
		SELECT mc.provider,
		       COUNT(*) AS request_count,
		       COALESCE(SUM(r.usd_cost), 0) AS total_usd
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.tenant_id = $1
		  AND r.request_at >= NOW() - $2::interval
		GROUP BY mc.provider
		ORDER BY total_usd DESC
	`, tenantID, interval)
	if err != nil {
		return nil, fmt.Errorf("procurement by provider: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ps ProviderSpend
		if err := rows.Scan(&ps.Provider, &ps.RequestCount, &ps.TotalUSD); err != nil {
			return nil, fmt.Errorf("procurement provider scan: %w", err)
		}
		ps.SharePercent = ProviderSharePercent(ps.TotalUSD, report.TotalUSD)
		report.ByProvider = append(report.ByProvider, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement provider rows: %w", err)
	}

	// MoM growth per provider: compare last full month vs month before.
	thisMonth := time.Now().UTC().Format("2006-01")
	lastMonth := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")
	twoMonthsAgo := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01")
	for i := range report.ByProvider {
		var curr, prev float64
		_ = s.pool.QueryRow(ctx, `
			SELECT
				COALESCE(SUM(CASE WHEN to_char(r.request_at AT TIME ZONE 'UTC','YYYY-MM') = $3 THEN r.usd_cost ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN to_char(r.request_at AT TIME ZONE 'UTC','YYYY-MM') = $4 THEN r.usd_cost ELSE 0 END), 0)
			FROM usage_records r
			JOIN model_configs mc ON mc.id = r.model_config_id
			WHERE r.tenant_id = $1 AND mc.provider = $2
			  AND to_char(r.request_at AT TIME ZONE 'UTC','YYYY-MM') IN ($3, $4)
		`, tenantID, report.ByProvider[i].Provider, lastMonth, twoMonthsAgo).Scan(&curr, &prev)
		_ = thisMonth
		report.ByProvider[i].MoMGrowthPercent = MoMGrowth(curr, prev)
	}

	// Top 10 models by spend.
	mrows, err := s.pool.Query(ctx, `
		SELECT mc.name, mc.provider, COUNT(*) AS request_count, COALESCE(SUM(r.usd_cost), 0) AS total_usd
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.tenant_id = $1
		  AND r.request_at >= NOW() - $2::interval
		GROUP BY mc.name, mc.provider
		ORDER BY total_usd DESC
		LIMIT 10
	`, tenantID, interval)
	if err != nil {
		return nil, fmt.Errorf("procurement top models: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var ms ModelSpend
		if err := mrows.Scan(&ms.Model, &ms.Provider, &ms.RequestCount, &ms.TotalUSD); err != nil {
			return nil, fmt.Errorf("procurement model scan: %w", err)
		}
		report.TopModels = append(report.TopModels, ms)
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("procurement model rows: %w", err)
	}

	// Peak month and average.
	prow, err := s.pool.Query(ctx, `
		SELECT to_char(request_at AT TIME ZONE 'UTC','YYYY-MM') AS month, COALESCE(SUM(usd_cost), 0) AS total
		FROM usage_records
		WHERE tenant_id = $1
		  AND request_at >= NOW() - $2::interval
		GROUP BY month
		ORDER BY total DESC
	`, tenantID, interval)
	if err != nil {
		return nil, fmt.Errorf("procurement peak: %w", err)
	}
	defer prow.Close()
	var monthCount int
	var totalForAvg float64
	for prow.Next() {
		var month string
		var total float64
		if err := prow.Scan(&month, &total); err != nil {
			return nil, fmt.Errorf("procurement peak scan: %w", err)
		}
		if monthCount == 0 {
			report.PeakMonthUSD = total
			report.PeakMonth = month
		}
		totalForAvg += total
		monthCount++
	}
	if err := prow.Err(); err != nil {
		return nil, fmt.Errorf("procurement peak rows: %w", err)
	}
	if monthCount > 0 {
		report.AvgMonthlyUSD = totalForAvg / float64(monthCount)
	}

	// EOY forecast reusing linear regression on monthly totals.
	y := make([]float64, monthCount)
	// Re-query monthly totals in order for regression.
	yrows, err := s.pool.Query(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND request_at >= NOW() - $2::interval
		GROUP BY to_char(request_at AT TIME ZONE 'UTC','YYYY-MM')
		ORDER BY to_char(request_at AT TIME ZONE 'UTC','YYYY-MM') ASC
	`, tenantID, interval)
	if err != nil {
		return nil, fmt.Errorf("procurement forecast: %w", err)
	}
	defer yrows.Close()
	idx := 0
	for yrows.Next() {
		if idx < len(y) {
			_ = yrows.Scan(&y[idx])
			idx++
		}
	}
	if err := yrows.Err(); err != nil {
		return nil, fmt.Errorf("procurement forecast rows: %w", err)
	}

	slope, intercept := LinearRegression(y)
	currentMonth := int(time.Now().UTC().Month())
	monthsToEOY := 12 - currentMonth
	lastIdx := float64(len(y) - 1)
	report.ForecastedEOY = totalForAvg * float64(currentMonth) / float64(len(y))
	for i := 1; i <= monthsToEOY; i++ {
		predicted := slope*(lastIdx+float64(i)) + intercept
		if predicted < 0 {
			predicted = 0
		}
		report.ForecastedEOY += predicted
	}

	if report.ByProvider == nil {
		report.ByProvider = []ProviderSpend{}
	}
	if report.TopModels == nil {
		report.TopModels = []ModelSpend{}
	}
	return report, nil
}
