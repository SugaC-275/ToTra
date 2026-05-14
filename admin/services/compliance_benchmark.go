package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func PercentileRank(value float64, all []float64) float64 {
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

type ComplianceBenchmark struct {
	YourRate         float64 `json:"your_rate"`
	P25              float64 `json:"p25"`
	P50              float64 `json:"p50"`
	P75              float64 `json:"p75"`
	TenantCount      int     `json:"tenant_count"`
	PercentileRank   float64 `json:"percentile_rank"`
	InsufficientData bool    `json:"insufficient_data"`
}

type ComplianceBenchmarkService struct {
	pool *pgxpool.Pool
}

func NewComplianceBenchmarkService(pool *pgxpool.Pool) *ComplianceBenchmarkService {
	return &ComplianceBenchmarkService{pool: pool}
}

func (s *ComplianceBenchmarkService) GetComplianceBenchmark(ctx context.Context, tenantID string) (*ComplianceBenchmark, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sub.tenant_id, sub.rate
		FROM (
			SELECT v.tenant_id,
			       COUNT(v.id)::float / NULLIF(r.total_requests, 0) * 1000 AS rate
			FROM (
				SELECT tenant_id, COUNT(*) AS id
				FROM pii_violations
				WHERE occurred_at >= NOW() - INTERVAL '30 days'
				GROUP BY tenant_id
			) v
			JOIN (
				SELECT tenant_id, COUNT(*) AS total_requests
				FROM usage_records
				WHERE request_at >= NOW() - INTERVAL '30 days'
				GROUP BY tenant_id
			) r ON r.tenant_id = v.tenant_id
		) sub`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allRates []float64
	yourRate := 0.0
	for rows.Next() {
		var tid string
		var rate float64
		if err := rows.Scan(&tid, &rate); err != nil {
			return nil, err
		}
		allRates = append(allRates, rate)
		if tid == tenantID {
			yourRate = rate
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const minTenants = 3
	if len(allRates) < minTenants {
		return &ComplianceBenchmark{
			YourRate:         yourRate,
			TenantCount:      len(allRates),
			InsufficientData: true,
		}, nil
	}

	var p25, p50, p75 float64
	err = s.pool.QueryRow(ctx, `
		SELECT
			PERCENTILE_CONT(0.25) WITHIN GROUP (ORDER BY rate),
			PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rate),
			PERCENTILE_CONT(0.75) WITHIN GROUP (ORDER BY rate)
		FROM (
			SELECT COUNT(v.id)::float / NULLIF(r.total_requests, 0) * 1000 AS rate
			FROM (
				SELECT tenant_id, COUNT(*) AS id FROM pii_violations
				WHERE occurred_at >= NOW() - INTERVAL '30 days' GROUP BY tenant_id
			) v
			JOIN (
				SELECT tenant_id, COUNT(*) AS total_requests FROM usage_records
				WHERE request_at >= NOW() - INTERVAL '30 days' GROUP BY tenant_id
			) r ON r.tenant_id = v.tenant_id
		) sub`,
	).Scan(&p25, &p50, &p75)
	if err != nil {
		return nil, err
	}

	return &ComplianceBenchmark{
		YourRate:       yourRate,
		P25:            p25,
		P50:            p50,
		P75:            p75,
		TenantCount:    len(allRates),
		PercentileRank: PercentileRank(yourRate, allRates),
	}, nil
}
