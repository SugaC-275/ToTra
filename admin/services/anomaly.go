package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	anomalyMinViolations   = 5
	anomalySpikeMultiplier = 3.0
	anomalyHighMultiplier  = 5.0
	anomalyBaselineMinimum = 1.0
)

func IsAnomaly(recentCount int, weeklyBaseline float64) bool {
	if recentCount < anomalyMinViolations {
		return false
	}
	if weeklyBaseline < anomalyBaselineMinimum {
		return true
	}
	return float64(recentCount) > weeklyBaseline*anomalySpikeMultiplier
}

func AnomalySeverity(recentCount int, weeklyBaseline float64) string {
	if weeklyBaseline < anomalyBaselineMinimum || float64(recentCount) > weeklyBaseline*anomalyHighMultiplier {
		return "high"
	}
	return "medium"
}

type AnomalyUser struct {
	UserID         string  `json:"user_id"`
	UserName       string  `json:"user_name"`
	UserEmail      string  `json:"user_email"`
	Department     string  `json:"department"`
	RecentCount    int     `json:"recent_count"`
	WeeklyBaseline float64 `json:"weekly_baseline"`
	Severity       string  `json:"severity"`
	DetectedAt     string  `json:"detected_at"`
}

type AnomalyService struct {
	pool *pgxpool.Pool
}

func NewAnomalyService(pool *pgxpool.Pool) *AnomalyService {
	return &AnomalyService{pool: pool}
}

func (s *AnomalyService) GetAnomalies(ctx context.Context, tenantID string) ([]*AnomalyUser, error) {
	rows, err := s.pool.Query(ctx, `
		WITH recent AS (
			SELECT user_id, COUNT(*) AS recent_count
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '7 days'
			GROUP BY user_id
		),
		baseline AS (
			SELECT user_id, COUNT(*)::float / 4.0 AS weekly_avg
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '30 days'
			GROUP BY user_id
		)
		SELECT r.user_id::text,
		       COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.department, ''),
		       r.recent_count,
		       COALESCE(b.weekly_avg, 0)
		FROM recent r
		LEFT JOIN baseline b ON b.user_id = r.user_id
		LEFT JOIN users u ON u.id = r.user_id
		WHERE r.recent_count >= $2
		  AND (b.weekly_avg IS NULL OR b.weekly_avg < 1 OR r.recent_count > b.weekly_avg * $3)
		ORDER BY r.recent_count DESC`,
		tenantID, anomalyMinViolations, anomalySpikeMultiplier,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var result []*AnomalyUser
	for rows.Next() {
		a := &AnomalyUser{DetectedAt: now}
		if err := rows.Scan(&a.UserID, &a.UserName, &a.UserEmail, &a.Department,
			&a.RecentCount, &a.WeeklyBaseline); err != nil {
			return nil, err
		}
		a.Severity = AnomalySeverity(a.RecentCount, a.WeeklyBaseline)
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []*AnomalyUser{}
	}
	return result, nil
}
