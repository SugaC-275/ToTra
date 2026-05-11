package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type QuarterROI struct {
	Quarter     string  `json:"quarter"`
	TotalUSD    float64 `json:"total_usd"`
	TotalOutput float64 `json:"total_output"`
	ROIScore    float64 `json:"roi_score"`
	ActiveUsers int     `json:"active_users"`
}

type BenchmarkResult struct {
	TenantAvgEfficiency float64 `json:"tenant_avg_efficiency"`
	Percentile          int     `json:"percentile"`
	IndustryP25         float64 `json:"industry_p25"`
	IndustryP50         float64 `json:"industry_p50"`
	IndustryP75         float64 `json:"industry_p75"`
	IndustryP90         float64 `json:"industry_p90"`
	Label               string  `json:"label"`
}

type DeptChallengeEntry struct {
	Department  string  `json:"department"`
	AvgAIQScore float64 `json:"avg_aiq_score"`
	UserCount   int     `json:"user_count"`
	Rank        int     `json:"rank"`
	IsWinner    bool    `json:"is_winner"`
}

func ComputeROI(output, usd float64) float64 {
	if usd == 0 {
		return 0
	}
	return output / usd
}

func ComputePercentile(score, p25, p50, p75, p90 float64) int {
	switch {
	case score >= p90:
		return 90
	case score >= p75:
		return 75
	case score >= p50:
		return 50
	case score >= p25:
		return 25
	default:
		return 10
	}
}

func BenchmarkLabel(pct int) string {
	switch pct {
	case 90:
		return "Top 10%"
	case 75:
		return "Top 25%"
	case 50:
		return "Top 50%"
	case 25:
		return "Bottom 50%"
	default:
		return "Bottom 25%"
	}
}

type ROIService struct {
	pool *pgxpool.Pool
}

func NewROIService(pool *pgxpool.Pool) *ROIService {
	return &ROIService{pool: pool}
}

func (s *ROIService) GetQuarterlyROI(ctx context.Context, tenantID, year string) ([]*QuarterROI, error) {
	costRows, err := s.pool.Query(ctx, `
		SELECT
			EXTRACT(QUARTER FROM request_at)::int AS q,
			COALESCE(SUM(usd_cost), 0)            AS total_usd,
			COUNT(DISTINCT user_id)               AS active_users
		FROM usage_records
		WHERE tenant_id = $1
		  AND EXTRACT(YEAR FROM request_at)::int = $2::int
		GROUP BY q
		ORDER BY q`,
		tenantID, year,
	)
	if err != nil {
		return nil, err
	}
	defer costRows.Close()

	type costRow struct {
		q           int
		totalUSD    float64
		activeUsers int
	}
	costMap := map[int]*costRow{}
	for costRows.Next() {
		var r costRow
		if err := costRows.Scan(&r.q, &r.totalUSD, &r.activeUsers); err != nil {
			return nil, err
		}
		costMap[r.q] = &r
	}
	if err := costRows.Err(); err != nil {
		return nil, err
	}

	outRows, err := s.pool.Query(ctx, `
		SELECT
			EXTRACT(QUARTER FROM to_date(year_month, 'YYYY-MM'))::int AS q,
			COALESCE(SUM(total_output_weight), 0)                     AS total_output
		FROM efficiency_snapshots
		WHERE tenant_id = $1
		  AND year_month LIKE $2 || '-%'
		  AND anomaly_flagged = false
		GROUP BY q
		ORDER BY q`,
		tenantID, year,
	)
	if err != nil {
		return nil, err
	}
	defer outRows.Close()

	outputMap := map[int]float64{}
	for outRows.Next() {
		var q int
		var out float64
		if err := outRows.Scan(&q, &out); err != nil {
			return nil, err
		}
		outputMap[q] = out
	}
	if err := outRows.Err(); err != nil {
		return nil, err
	}

	seen := map[int]bool{}
	for q := range costMap {
		seen[q] = true
	}
	for q := range outputMap {
		seen[q] = true
	}

	var result []*QuarterROI
	for q := 1; q <= 4; q++ {
		if !seen[q] {
			continue
		}
		var totalUSD float64
		var activeUsers int
		if cr, ok := costMap[q]; ok {
			totalUSD = cr.totalUSD
			activeUsers = cr.activeUsers
		}
		totalOutput := outputMap[q]
		result = append(result, &QuarterROI{
			Quarter:     year + "-Q" + string(rune('0'+q)),
			TotalUSD:    totalUSD,
			TotalOutput: totalOutput,
			ROIScore:    ComputeROI(totalOutput, totalUSD),
			ActiveUsers: activeUsers,
		})
	}
	return result, nil
}

func (s *ROIService) GetBenchmark(ctx context.Context, tenantID, yearMonth string) (*BenchmarkResult, error) {
	var tenantAvg float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(efficiency_score), 0)
		FROM efficiency_snapshots
		WHERE tenant_id = $1 AND year_month = $2 AND anomaly_flagged = false`,
		tenantID, yearMonth,
	).Scan(&tenantAvg)
	if err != nil {
		return nil, err
	}

	var p25, p50, p75, p90 float64
	err = s.pool.QueryRow(ctx,
		`SELECT p25, p50, p75, p90 FROM industry_benchmarks ORDER BY id DESC LIMIT 1`,
	).Scan(&p25, &p50, &p75, &p90)
	if err != nil {
		return nil, err
	}

	pct := ComputePercentile(tenantAvg, p25, p50, p75, p90)
	return &BenchmarkResult{
		TenantAvgEfficiency: tenantAvg,
		Percentile:          pct,
		IndustryP25:         p25,
		IndustryP50:         p50,
		IndustryP75:         p75,
		IndustryP90:         p90,
		Label:               BenchmarkLabel(pct),
	}, nil
}

func (s *ROIService) GetDeptChallenge(ctx context.Context, tenantID, yearMonth string) ([]*DeptChallengeEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(u.department, '(未设置)') AS department,
			COALESCE(AVG(s.aiq_score), 0)     AS avg_aiq_score,
			COUNT(DISTINCT s.user_id)          AS user_count
		FROM efficiency_snapshots s
		JOIN users u ON u.id = s.user_id
		WHERE s.tenant_id = $1
		  AND s.year_month = $2
		  AND s.anomaly_flagged = false
		GROUP BY COALESCE(u.department, '(未设置)')
		ORDER BY avg_aiq_score DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DeptChallengeEntry
	for rows.Next() {
		e := &DeptChallengeEntry{}
		if err := rows.Scan(&e.Department, &e.AvgAIQScore, &e.UserCount); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, e := range result {
		e.Rank = i + 1
		e.IsWinner = (i == 0)
	}
	return result, nil
}
