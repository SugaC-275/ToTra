package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SpendChange returns the dollar difference from previous to current.
// Returns 0 when previous is 0 to avoid misleading percentages.
func SpendChange(current, previous float64) float64 {
	if previous == 0 {
		return 0
	}
	return current - previous
}

// CrossedThresholds returns which of [50, 80, 100] percent thresholds are crossed by usedPercent.
func CrossedThresholds(usedPercent float64) []int {
	thresholds := []int{50, 80, 100}
	var crossed []int
	for _, t := range thresholds {
		if usedPercent >= float64(t) {
			crossed = append(crossed, t)
		}
	}
	return crossed
}

type SpenderEntry struct {
	UserID      string  `json:"user_id"`
	Email       string  `json:"email"`
	CurrentUSD  float64 `json:"current_usd"`
	PreviousUSD float64 `json:"previous_usd"`
	ChangeUSD   float64 `json:"change_usd"`
}

type TopSpendersReport struct {
	TenantID    string         `json:"tenant_id"`
	YearMonth   string         `json:"year_month"`
	Entries     []SpenderEntry `json:"entries"`
	GeneratedAt string         `json:"generated_at"`
}

type TopSpendersService struct{ pool *pgxpool.Pool }

func NewTopSpendersService(pool *pgxpool.Pool) *TopSpendersService {
	return &TopSpendersService{pool: pool}
}

func (s *TopSpendersService) GetTopSpenders(ctx context.Context, tenantID, yearMonth string, limit int) (*TopSpendersReport, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}
	if limit <= 0 {
		limit = 10
	}

	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, fmt.Errorf("invalid month format %q: %w", yearMonth, err)
	}
	prevMonth := t.AddDate(0, -1, 0).Format("2006-01")

	rows, err := s.pool.Query(ctx, `
		WITH cur AS (
			SELECT ur.user_id, u.email, SUM(ur.usd_cost) AS usd
			FROM usage_records ur
			JOIN users u ON u.id = ur.user_id
			WHERE ur.tenant_id = $1
			  AND to_char(ur.request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
			GROUP BY ur.user_id, u.email
		),
		prev AS (
			SELECT ur.user_id, SUM(ur.usd_cost) AS usd
			FROM usage_records ur
			WHERE ur.tenant_id = $1
			  AND to_char(ur.request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $3
			GROUP BY ur.user_id
		)
		SELECT c.user_id::text, c.email, c.usd AS current_usd, COALESCE(p.usd, 0) AS previous_usd
		FROM cur c
		LEFT JOIN prev p ON p.user_id = c.user_id
		ORDER BY c.usd DESC
		LIMIT $4
	`, tenantID, yearMonth, prevMonth, limit)
	if err != nil {
		return nil, fmt.Errorf("top spenders query: %w", err)
	}
	defer rows.Close()

	var entries []SpenderEntry
	for rows.Next() {
		var e SpenderEntry
		if err := rows.Scan(&e.UserID, &e.Email, &e.CurrentUSD, &e.PreviousUSD); err != nil {
			return nil, fmt.Errorf("top spenders scan: %w", err)
		}
		e.ChangeUSD = SpendChange(e.CurrentUSD, e.PreviousUSD)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top spenders rows: %w", err)
	}
	if entries == nil {
		entries = []SpenderEntry{}
	}

	return &TopSpendersReport{
		TenantID:    tenantID,
		YearMonth:   yearMonth,
		Entries:     entries,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
