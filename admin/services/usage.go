package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

type DeptSummary struct {
	Department   string  `json:"department"`
	UserCount    int     `json:"user_count"`
	ActiveUsers  int     `json:"active_users"`
	TotalSCU     float64 `json:"total_scu"`
	TotalUSD     float64 `json:"total_usd"`
	RequestCount int     `json:"request_count"`
}

type BudgetForecast struct {
	Month         string  `json:"month"`
	CurrentSCU    float64 `json:"current_scu"`
	CurrentUSD    float64 `json:"current_usd"`
	DaysElapsed   int     `json:"days_elapsed"`
	DaysInMonth   int     `json:"days_in_month"`
	ProjectedSCU  float64 `json:"projected_scu"`
	ProjectedUSD  float64 `json:"projected_usd"`
	PriorMonthSCU float64 `json:"prior_month_scu"`
	TrendPct      float64 `json:"trend_pct"`
}

type InactiveUser struct {
	UserID       string  `json:"user_id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Department   string  `json:"department"`
	JobRole      string  `json:"job_role"`
	ActiveDays   int     `json:"active_days"`
	LastActiveAt *string `json:"last_active_at"` // "2006-01-02" or null
}

type UsageServiceInterface interface {
	GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error)
	GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error)
	GetDepartmentSummary(ctx context.Context, tenantID, yearMonth string) ([]*DeptSummary, error)
	GetBudgetForecast(ctx context.Context, tenantID, yearMonth string) (*BudgetForecast, error)
	GetInactiveUsers(ctx context.Context, tenantID, yearMonth string, maxDays int) ([]*InactiveUser, error)
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

func (s *UsageService) GetDepartmentSummary(ctx context.Context, tenantID, yearMonth string) ([]*DeptSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(u.department, '(未设置)') AS department,
			COUNT(DISTINCT u.id)               AS user_count,
			COUNT(DISTINCT r.user_id)          AS active_users,
			COALESCE(SUM(r.scu_cost), 0)       AS total_scu,
			COALESCE(SUM(r.usd_cost), 0)       AS total_usd,
			COUNT(r.id)                        AS request_count
		FROM users u
		LEFT JOIN usage_records r
			ON r.user_id = u.id
			AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true
		GROUP BY COALESCE(u.department, '(未设置)')
		ORDER BY total_scu DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*DeptSummary
	for rows.Next() {
		d := &DeptSummary{}
		if err := rows.Scan(&d.Department, &d.UserCount, &d.ActiveUsers,
			&d.TotalSCU, &d.TotalUSD, &d.RequestCount); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *UsageService) GetBudgetForecast(ctx context.Context, tenantID, yearMonth string) (*BudgetForecast, error) {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, err
	}
	daysInMonth := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()

	now := time.Now().UTC()
	daysElapsed := daysInMonth // past month: use full month
	if yearMonth == now.Format("2006-01") {
		daysElapsed = now.Day()
	}

	var currentSCU, currentUSD float64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(scu_cost),0), COALESCE(SUM(usd_cost),0)
		 FROM usage_records WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2`,
		tenantID, yearMonth).Scan(&currentSCU, &currentUSD); err != nil {
		return nil, err
	}

	priorMonth := t.AddDate(0, -1, 0).Format("2006-01")
	var priorMonthSCU float64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(scu_cost),0)
		 FROM usage_records WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2`,
		tenantID, priorMonth).Scan(&priorMonthSCU); err != nil {
		return nil, err
	}

	var projSCU, projUSD float64
	if daysElapsed > 0 {
		projSCU = currentSCU / float64(daysElapsed) * float64(daysInMonth)
		projUSD = currentUSD / float64(daysElapsed) * float64(daysInMonth)
	}

	var trendPct float64
	if priorMonthSCU > 0 {
		trendPct = (projSCU - priorMonthSCU) / priorMonthSCU * 100
	}

	return &BudgetForecast{
		Month:         yearMonth,
		CurrentSCU:    currentSCU,
		CurrentUSD:    currentUSD,
		DaysElapsed:   daysElapsed,
		DaysInMonth:   daysInMonth,
		ProjectedSCU:  projSCU,
		ProjectedUSD:  projUSD,
		PriorMonthSCU: priorMonthSCU,
		TrendPct:      trendPct,
	}, nil
}

func (s *UsageService) GetInactiveUsers(ctx context.Context, tenantID, yearMonth string, maxDays int) ([]*InactiveUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name, u.email,
		       COALESCE(u.department, ''),
		       COALESCE(u.job_role, ''),
		       COUNT(DISTINCT DATE(r.request_at)) AS active_days,
		       MAX(r.request_at)                  AS last_active_at
		FROM users u
		LEFT JOIN usage_records r
			ON r.user_id = u.id
			AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true AND u.role != 'admin'
		GROUP BY u.id, u.name, u.email, u.department, u.job_role
		HAVING COUNT(DISTINCT DATE(r.request_at)) < $3
		ORDER BY active_days ASC, u.name`,
		tenantID, yearMonth, maxDays,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*InactiveUser
	for rows.Next() {
		u := &InactiveUser{}
		var lastAt pgtype.Timestamptz
		if err := rows.Scan(&u.UserID, &u.Name, &u.Email,
			&u.Department, &u.JobRole, &u.ActiveDays, &lastAt); err != nil {
			return nil, err
		}
		if lastAt.Valid {
			dateStr := lastAt.Time.Format("2006-01-02")
			u.LastActiveAt = &dateStr
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
