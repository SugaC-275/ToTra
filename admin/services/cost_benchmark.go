package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CostPercentileRank returns what percentage of all values are strictly below value.
func CostPercentileRank(value float64, all []float64) float64 {
	if len(all) == 0 {
		return 0
	}
	below := 0
	for _, v := range all {
		if v < value {
			below++
		}
	}
	return float64(below) / float64(len(all)) * 100
}

type CostBenchmark struct {
	YearMonth        string  `json:"year_month"`
	TenantPerUserUSD float64 `json:"tenant_per_user_usd"`
	P25              float64 `json:"p25"`
	P50              float64 `json:"p50"`
	P75              float64 `json:"p75"`
	TenantCount      int     `json:"tenant_count"`
	PercentileRank   float64 `json:"percentile_rank"`
	InsufficientData bool    `json:"insufficient_data"`
}

type ModelROI struct {
	Model            string  `json:"model"`
	TotalRequests    int64   `json:"total_requests"`
	USDPer1KRequests float64 `json:"usd_per_1k_requests"`
}

type CostBenchmarkService struct{ pool *pgxpool.Pool }

func NewCostBenchmarkService(pool *pgxpool.Pool) *CostBenchmarkService {
	return &CostBenchmarkService{pool: pool}
}

func (s *CostBenchmarkService) GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*CostBenchmark, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}

	var tenantPerUser float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost) / NULLIF(COUNT(DISTINCT user_id), 0), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&tenantPerUser)
	if err != nil {
		return nil, fmt.Errorf("cost benchmark tenant query: %w", err)
	}

	var p25, p50, p75 float64
	var tenantCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(PERCENTILE_CONT(0.25) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COALESCE(PERCENTILE_CONT(0.75) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COUNT(*)
		FROM (
			SELECT tenant_id, SUM(usd_cost) / NULLIF(COUNT(DISTINCT user_id), 0) AS per_user_usd
			FROM usage_records
			WHERE to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $1
			  AND tenant_id != $2
			GROUP BY tenant_id
		) sub
	`, yearMonth, tenantID).Scan(&p25, &p50, &p75, &tenantCount)
	if err != nil {
		return nil, fmt.Errorf("cost benchmark cross-tenant query: %w", err)
	}

	if tenantCount < 3 {
		return &CostBenchmark{
			YearMonth:        yearMonth,
			TenantPerUserUSD: tenantPerUser,
			InsufficientData: true,
		}, nil
	}

	return &CostBenchmark{
		YearMonth:        yearMonth,
		TenantPerUserUSD: tenantPerUser,
		P25:              p25,
		P50:              p50,
		P75:              p75,
		TenantCount:      tenantCount,
		PercentileRank:   CostPercentileRank(tenantPerUser, []float64{p25, p50, p75}),
		InsufficientData: false,
	}, nil
}

func (s *CostBenchmarkService) GetModelROI(ctx context.Context) ([]ModelROI, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mc.name AS model,
		       COUNT(*) AS total_requests,
		       SUM(r.usd_cost) / COUNT(*) * 1000 AS usd_per_1k_requests
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.request_at >= NOW() - INTERVAL '30 days'
		GROUP BY mc.name
		HAVING COUNT(*) >= 100
		ORDER BY usd_per_1k_requests ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("model roi query: %w", err)
	}
	defer rows.Close()

	var results []ModelROI
	for rows.Next() {
		var roi ModelROI
		if err := rows.Scan(&roi.Model, &roi.TotalRequests, &roi.USDPer1KRequests); err != nil {
			return nil, fmt.Errorf("model roi scan: %w", err)
		}
		results = append(results, roi)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("model roi rows: %w", err)
	}
	return results, nil
}
