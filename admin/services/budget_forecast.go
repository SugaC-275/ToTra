package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LinearRegression fits y = slope*x + intercept to y[0..n-1] using OLS.
func LinearRegression(y []float64) (slope, intercept float64) {
	n := float64(len(y))
	if n == 0 {
		return 0, 0
	}
	if n == 1 {
		return 0, y[0]
	}
	var sumX, sumY, sumXY, sumX2 float64
	for i, v := range y {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n
	return slope, intercept
}

type MonthlySpend struct {
	Month    string  `json:"month"`
	TotalUSD float64 `json:"total_usd"`
}

type EOYBudgetForecast struct {
	TenantID      string         `json:"tenant_id"`
	History       []MonthlySpend `json:"history"`
	ForecastedEOY float64        `json:"forecasted_eoy_usd"`
	GeneratedAt   string         `json:"generated_at"`
}

type BudgetForecastService struct{ pool *pgxpool.Pool }

func NewBudgetForecastService(pool *pgxpool.Pool) *BudgetForecastService {
	return &BudgetForecastService{pool: pool}
}

func (s *BudgetForecastService) GetBudgetForecast(ctx context.Context, tenantID string) (*EOYBudgetForecast, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') AS month,
		       SUM(usd_cost) AS total_usd
		FROM usage_records
		WHERE tenant_id = $1
		  AND request_at >= NOW() - INTERVAL '6 months'
		GROUP BY month
		ORDER BY month ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("budget forecast query: %w", err)
	}
	defer rows.Close()

	var history []MonthlySpend
	for rows.Next() {
		var ms MonthlySpend
		if err := rows.Scan(&ms.Month, &ms.TotalUSD); err != nil {
			return nil, fmt.Errorf("budget forecast scan: %w", err)
		}
		history = append(history, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("budget forecast rows: %w", err)
	}

	y := make([]float64, len(history))
	for i, m := range history {
		y[i] = m.TotalUSD
	}
	slope, intercept := LinearRegression(y)

	currentMonth := time.Now().UTC().Month()
	monthsToEOY := 12 - int(currentMonth)
	lastIndex := float64(len(history) - 1)

	var forecastedEOY float64
	for _, m := range history {
		forecastedEOY += m.TotalUSD
	}
	for i := 1; i <= monthsToEOY; i++ {
		predicted := slope*(lastIndex+float64(i)) + intercept
		if predicted < 0 {
			predicted = 0
		}
		forecastedEOY += predicted
	}

	return &EOYBudgetForecast{
		TenantID:      tenantID,
		History:       history,
		ForecastedEOY: forecastedEOY,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}
