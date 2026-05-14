package services

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ViolationRecord represents a single PII violation event.
type ViolationRecord struct {
	ID          int64     `json:"id"`
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	UserName    string    `json:"user_name"`
	UserEmail   string    `json:"user_email"`
	Department  string    `json:"department"`
	PIIType     string    `json:"pii_type"`
	Action      string    `json:"action"`
	RequestPath string    `json:"request_path"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// UserRiskScore holds the aggregated risk info for a single user in a month.
type UserRiskScore struct {
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	UserEmail      string `json:"user_email"`
	Department     string `json:"department"`
	ViolationCount int    `json:"violation_count"`
	RiskScore      int    `json:"risk_score"`
	RiskLevelLabel string `json:"risk_level"`
}

// PIITypeStat holds a count per PII type.
type PIITypeStat struct {
	PIIType string `json:"pii_type"`
	Count   int    `json:"count"`
}

// ComplianceReport is the monthly compliance summary.
type ComplianceReport struct {
	YearMonth      string        `json:"year_month"`
	TotalViolations int          `json:"total_violations"`
	UniqueUsers    int           `json:"unique_users"`
	TopPIITypes    []PIITypeStat `json:"top_pii_types"`
	HighRiskUsers  []UserRiskScore `json:"high_risk_users"`
}

// ComputeRiskScore computes a 0–100 score using a logarithmic formula.
// violations: number of PII violations; totalRequests: total AI requests in period.
// ComputeRiskScore computes a 0–100 score using a logarithmic formula.
// violations: number of PII violations; totalRequests: total AI requests in period.
// Formula: 100 * log1p(ratio * 50) / log1p(50), capped at 100.
func ComputeRiskScore(violations int, totalRequests int64) int {
	if violations == 0 {
		return 0
	}
	denom := totalRequests
	if denom <= 0 {
		denom = 1
	}
	ratio := float64(violations) / float64(denom)
	const k = 50.0
	score := 100.0 * math.Log1p(ratio*k) / math.Log1p(k)
	if score > 100 {
		score = 100
	}
	return int(math.Round(score))
}

// RiskLevel converts a numeric score to a label.
func RiskLevel(score int) string {
	switch {
	case score >= 90:
		return "critical"
	case score >= 70:
		return "high"
	case score >= 30:
		return "medium"
	default:
		return "low"
	}
}

// ComplianceService provides compliance data queries.
type ComplianceService struct {
	pool *pgxpool.Pool
}

// NewComplianceService creates a new ComplianceService.
func NewComplianceService(pool *pgxpool.Pool) *ComplianceService {
	return &ComplianceService{pool: pool}
}

// GetViolations returns PII violations for a tenant in the given yearMonth (YYYY-MM), up to limit rows.
func (s *ComplianceService) GetViolations(ctx context.Context, tenantID, yearMonth string, limit int) ([]*ViolationRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT v.id, v.tenant_id,
		        COALESCE(v.user_id::text, ''), COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.department, ''),
		        v.pii_type, v.action, COALESCE(v.request_path, ''), v.occurred_at
		 FROM pii_violations v
		 LEFT JOIN users u ON u.id = v.user_id
		 WHERE v.tenant_id = $1
		   AND to_char(v.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		 ORDER BY v.occurred_at DESC
		 LIMIT $3`,
		tenantID, yearMonth, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*ViolationRecord
	for rows.Next() {
		var r ViolationRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.UserID, &r.UserName, &r.UserEmail, &r.Department,
			&r.PIIType, &r.Action, &r.RequestPath, &r.OccurredAt); err != nil {
			return nil, err
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// GetRiskScores returns per-user risk scores for a tenant in the given yearMonth.
func (s *ComplianceService) GetRiskScores(ctx context.Context, tenantID, yearMonth string) ([]*UserRiskScore, error) {
	// Get violation counts per user
	rows, err := s.pool.Query(ctx,
		`SELECT v.user_id::text, COALESCE(u.name,''), COALESCE(u.email,''), COALESCE(u.department,''),
		        COUNT(*) AS violation_count
		 FROM pii_violations v
		 LEFT JOIN users u ON u.id = v.user_id
		 WHERE v.tenant_id = $1
		   AND to_char(v.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		   AND v.user_id IS NOT NULL
		 GROUP BY v.user_id, u.name, u.email, u.department
		 ORDER BY violation_count DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect user IDs to look up their total request counts
	type userRow struct {
		UserID     string
		UserName   string
		UserEmail  string
		Department string
		Violations int
	}
	var userRows []userRow
	for rows.Next() {
		var r userRow
		if err := rows.Scan(&r.UserID, &r.UserName, &r.UserEmail, &r.Department, &r.Violations); err != nil {
			return nil, err
		}
		userRows = append(userRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var result []*UserRiskScore
	for _, ur := range userRows {
		// Get total requests for this user in the month
		var totalReqs int64
		_ = s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM usage_records
			 WHERE tenant_id = $1 AND user_id = $2
			   AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $3`,
			tenantID, ur.UserID, yearMonth,
		).Scan(&totalReqs)

		score := ComputeRiskScore(ur.Violations, totalReqs)
		result = append(result, &UserRiskScore{
			UserID:         ur.UserID,
			UserName:       ur.UserName,
			UserEmail:      ur.UserEmail,
			Department:     ur.Department,
			ViolationCount: ur.Violations,
			RiskScore:      score,
			RiskLevelLabel: RiskLevel(score),
		})
	}
	return result, nil
}

// GetMonthlyReport returns an aggregated compliance report for the given yearMonth.
func (s *ComplianceService) GetMonthlyReport(ctx context.Context, tenantID, yearMonth string) (*ComplianceReport, error) {
	report := &ComplianceReport{YearMonth: yearMonth}

	// Total violations and unique users
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT user_id)
		 FROM pii_violations
		 WHERE tenant_id = $1
		   AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2`,
		tenantID, yearMonth,
	).Scan(&report.TotalViolations, &report.UniqueUsers)
	if err != nil {
		return nil, err
	}

	// Top PII types
	piiRows, err := s.pool.Query(ctx,
		`SELECT pii_type, COUNT(*) AS cnt
		 FROM pii_violations
		 WHERE tenant_id = $1
		   AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		 GROUP BY pii_type
		 ORDER BY cnt DESC
		 LIMIT 10`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer piiRows.Close()
	for piiRows.Next() {
		var stat PIITypeStat
		if err := piiRows.Scan(&stat.PIIType, &stat.Count); err != nil {
			return nil, err
		}
		report.TopPIITypes = append(report.TopPIITypes, stat)
	}
	if err := piiRows.Err(); err != nil {
		return nil, err
	}

	// High-risk users (score >= 70)
	riskScores, err := s.GetRiskScores(ctx, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	for _, rs := range riskScores {
		if rs.RiskScore >= 70 {
			report.HighRiskUsers = append(report.HighRiskUsers, *rs)
		}
	}

	if report.TopPIITypes == nil {
		report.TopPIITypes = []PIITypeStat{}
	}
	if report.HighRiskUsers == nil {
		report.HighRiskUsers = []UserRiskScore{}
	}

	return report, nil
}
