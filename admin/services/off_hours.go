package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func IsWorkHour(hour, workStart, workEnd int) bool {
	return hour >= workStart && hour < workEnd
}

type HourlySpend struct {
	Hour         int     `json:"hour"`
	RequestCount int64   `json:"request_count"`
	SpentUSD     float64 `json:"spent_usd"`
	IsWorkHour   bool    `json:"is_work_hour"`
}

type OffHoursReport struct {
	TenantID        string        `json:"tenant_id"`
	YearMonth       string        `json:"year_month"`
	WorkStartHour   int           `json:"work_start_hour"`
	WorkEndHour     int           `json:"work_end_hour"`
	TotalUSD        float64       `json:"total_usd"`
	OffHoursUSD     float64       `json:"off_hours_usd"`
	OffHoursPercent float64       `json:"off_hours_percent"`
	HourlyBreakdown []HourlySpend `json:"hourly_breakdown"`
	GeneratedAt     string        `json:"generated_at"`
}

type OffHoursService struct{ pool *pgxpool.Pool }

func NewOffHoursService(pool *pgxpool.Pool) *OffHoursService {
	return &OffHoursService{pool: pool}
}

func (s *OffHoursService) GetOffHoursReport(ctx context.Context, tenantID, yearMonth string) (*OffHoursReport, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}
	workStart, workEnd := 9, 18
	_ = s.pool.QueryRow(ctx, `
		SELECT work_start_hour, work_end_hour FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&workStart, &workEnd)

	rows, err := s.pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM request_at AT TIME ZONE 'UTC')::int AS hour,
		       COUNT(*) AS request_count,
		       COALESCE(SUM(usd_cost), 0) AS spent_usd
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY hour
		ORDER BY hour ASC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("off hours query: %w", err)
	}
	defer rows.Close()

	hourMap := make(map[int]HourlySpend, 24)
	for rows.Next() {
		var h HourlySpend
		if err := rows.Scan(&h.Hour, &h.RequestCount, &h.SpentUSD); err != nil {
			return nil, fmt.Errorf("off hours scan: %w", err)
		}
		h.IsWorkHour = IsWorkHour(h.Hour, workStart, workEnd)
		hourMap[h.Hour] = h
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("off hours rows: %w", err)
	}

	breakdown := make([]HourlySpend, 24)
	var totalUSD, offHoursUSD float64
	for i := 0; i < 24; i++ {
		h := hourMap[i]
		h.Hour = i
		h.IsWorkHour = IsWorkHour(i, workStart, workEnd)
		breakdown[i] = h
		totalUSD += h.SpentUSD
		if !h.IsWorkHour {
			offHoursUSD += h.SpentUSD
		}
	}

	offPct := 0.0
	if totalUSD > 0 {
		offPct = offHoursUSD / totalUSD * 100
	}
	return &OffHoursReport{
		TenantID:        tenantID,
		YearMonth:       yearMonth,
		WorkStartHour:   workStart,
		WorkEndHour:     workEnd,
		TotalUSD:        totalUSD,
		OffHoursUSD:     offHoursUSD,
		OffHoursPercent: offPct,
		HourlyBreakdown: breakdown,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}
