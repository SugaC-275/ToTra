package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserMonthlySummary struct {
	UserID       string  `json:"user_id"`
	UserName     string  `json:"user_name"`
	TotalSCU     float64 `json:"total_scu"`
	TotalUSD     float64 `json:"total_usd"`
	RequestCount int     `json:"request_count"`
}

type AdoptionStats struct {
	TotalUsers   int     `json:"total_users"`
	ActiveUsers  int     `json:"active_users"`
	AdoptionRate float64 `json:"adoption_rate"`
}

type UsageServiceInterface interface {
	GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error)
	GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error)
}

type UsageService struct {
	pool *pgxpool.Pool
}

func NewUsageService(pool *pgxpool.Pool) *UsageService {
	return &UsageService{pool: pool}
}

func (s *UsageService) GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name,
		       COALESCE(SUM(r.scu_cost), 0) AS total_scu,
		       COALESCE(SUM(r.usd_cost), 0) AS total_usd,
		       COUNT(r.id) AS request_count
		FROM users u
		LEFT JOIN usage_records r ON r.user_id = u.id
		    AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1
		GROUP BY u.id, u.name
		ORDER BY total_scu DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*UserMonthlySummary
	for rows.Next() {
		sum := &UserMonthlySummary{}
		rows.Scan(&sum.UserID, &sum.UserName, &sum.TotalSCU, &sum.TotalUSD, &sum.RequestCount)
		summaries = append(summaries, sum)
	}
	return summaries, nil
}

func (s *UsageService) GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error) {
	var stats AdoptionStats
	err := s.pool.QueryRow(ctx, `
		SELECT
		    COUNT(DISTINCT u.id) AS total_users,
		    COUNT(DISTINCT r.user_id) AS active_users
		FROM users u
		LEFT JOIN usage_records r ON r.user_id = u.id
		    AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true`,
		tenantID, yearMonth,
	).Scan(&stats.TotalUsers, &stats.ActiveUsers)
	if err != nil {
		return nil, err
	}
	if stats.TotalUsers > 0 {
		stats.AdoptionRate = float64(stats.ActiveUsers) / float64(stats.TotalUsers)
	}
	return &stats, nil
}
