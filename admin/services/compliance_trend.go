package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MonthlyRiskScore converts violations-per-1000-requests to a 0–100 risk score.
// Rate of 50/1k = score 100 (saturates).
func MonthlyRiskScore(violationsPer1K float64) float64 {
	if violationsPer1K <= 0 {
		return 0
	}
	score := violationsPer1K * 2
	if score > 100 {
		return 100
	}
	return score
}

type MonthlyRiskPoint struct {
	Month      string  `json:"month"`
	Violations int64   `json:"violations"`
	Requests   int64   `json:"requests"`
	RatePer1K  float64 `json:"rate_per_1k"`
	RiskScore  float64 `json:"risk_score"`
}

type RiskTrend struct {
	TenantID    string             `json:"tenant_id"`
	Points      []MonthlyRiskPoint `json:"points"`
	GeneratedAt string             `json:"generated_at"`
}

type RiskTrendService struct{ pool *pgxpool.Pool }

func NewRiskTrendService(pool *pgxpool.Pool) *RiskTrendService {
	return &RiskTrendService{pool: pool}
}

func (s *RiskTrendService) GetRiskTrend(ctx context.Context, tenantID string) (*RiskTrend, error) {
	rows, err := s.pool.Query(ctx, `
		WITH months AS (
			SELECT to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') AS month,
			       COUNT(*) AS violations
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '6 months'
			GROUP BY month
		),
		reqs AS (
			SELECT to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') AS month,
			       COUNT(*) AS requests
			FROM usage_records
			WHERE tenant_id = $1
			  AND request_at >= NOW() - INTERVAL '6 months'
			GROUP BY month
		)
		SELECT COALESCE(m.month, r.month),
		       COALESCE(m.violations, 0),
		       COALESCE(r.requests, 0)
		FROM months m
		FULL OUTER JOIN reqs r ON r.month = m.month
		ORDER BY 1 ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("risk trend query: %w", err)
	}
	defer rows.Close()

	var points []MonthlyRiskPoint
	for rows.Next() {
		var p MonthlyRiskPoint
		if err := rows.Scan(&p.Month, &p.Violations, &p.Requests); err != nil {
			return nil, fmt.Errorf("risk trend scan: %w", err)
		}
		if p.Requests > 0 {
			p.RatePer1K = float64(p.Violations) / float64(p.Requests) * 1000
		}
		p.RiskScore = MonthlyRiskScore(p.RatePer1K)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("risk trend rows: %w", err)
	}
	if points == nil {
		points = []MonthlyRiskPoint{}
	}
	return &RiskTrend{
		TenantID:    tenantID,
		Points:      points,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Compliance Digest ---

// PIITypeStat is already defined in compliance.go; reused here.

type UserViolationStat struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	UserEmail  string `json:"user_email"`
	Department string `json:"department"`
	Count      int64  `json:"count"`
}

type ComplianceDigest struct {
	TenantID        string              `json:"tenant_id"`
	Period          string              `json:"period"`
	TotalViolations int64               `json:"total_violations"`
	TotalRequests   int64               `json:"total_requests"`
	BlockRate       float64             `json:"block_rate_per_1k"`
	ByPIIType       []DigestPIITypeStat `json:"by_pii_type"`
	TopViolators    []UserViolationStat `json:"top_violators"`
	GeneratedAt     string              `json:"generated_at"`
}

// DigestPIITypeStat is a per-PII-type count for the compliance digest (uses int64).
type DigestPIITypeStat struct {
	PIIType string `json:"pii_type"`
	Count   int64  `json:"count"`
}

type ComplianceDigestService struct{ pool *pgxpool.Pool }

func NewComplianceDigestService(pool *pgxpool.Pool) *ComplianceDigestService {
	return &ComplianceDigestService{pool: pool}
}

func (s *ComplianceDigestService) GetDigest(ctx context.Context, tenantID, yearMonth string) (*ComplianceDigest, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}

	digest := &ComplianceDigest{
		TenantID:    tenantID,
		Period:      yearMonth,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Total violations
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pii_violations
		WHERE tenant_id = $1
		  AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&digest.TotalViolations); err != nil {
		return nil, fmt.Errorf("digest total violations: %w", err)
	}

	// Total requests
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&digest.TotalRequests); err != nil {
		return nil, fmt.Errorf("digest total requests: %w", err)
	}

	if digest.TotalRequests > 0 {
		digest.BlockRate = float64(digest.TotalViolations) / float64(digest.TotalRequests) * 1000
	}

	// By PII type
	rows, err := s.pool.Query(ctx, `
		SELECT pii_type, COUNT(*) AS cnt
		FROM pii_violations
		WHERE tenant_id = $1
		  AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY pii_type ORDER BY cnt DESC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("digest by pii type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st DigestPIITypeStat
		if err := rows.Scan(&st.PIIType, &st.Count); err != nil {
			return nil, err
		}
		digest.ByPIIType = append(digest.ByPIIType, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if digest.ByPIIType == nil {
		digest.ByPIIType = []DigestPIITypeStat{}
	}

	// Top violators
	rows2, err := s.pool.Query(ctx, `
		SELECT v.user_id::text,
		       COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.department, ''),
		       COUNT(*) AS cnt
		FROM pii_violations v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.tenant_id = $1
		  AND to_char(v.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY v.user_id, u.name, u.email, u.department
		ORDER BY cnt DESC LIMIT 5
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("digest top violators: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var st UserViolationStat
		if err := rows2.Scan(&st.UserID, &st.UserName, &st.UserEmail, &st.Department, &st.Count); err != nil {
			return nil, err
		}
		digest.TopViolators = append(digest.TopViolators, st)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}
	if digest.TopViolators == nil {
		digest.TopViolators = []UserViolationStat{}
	}

	return digest, nil
}
