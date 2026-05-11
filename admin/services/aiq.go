package services

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RawAIQMetrics holds un-normalized per-user metrics for a month.
type RawAIQMetrics struct {
	UserID           string
	OutputDensity    float64 // median(completion/prompt) per conversation
	UsageConsistency float64 // active_days / working_days
	TaskDepth        float64 // median(turns) * log(avg_tokens+1)
	CostEfficiency   float64 // completion_tokens / scu_cost
	ActiveDays       int
}

type AIQService struct {
	pool *pgxpool.Pool
}

func NewAIQService(pool *pgxpool.Pool) *AIQService {
	return &AIQService{pool: pool}
}

// GetRawMetrics fetches batch AIQ metrics for all users in a tenant for yearMonth (e.g. "2026-05").
func (s *AIQService) GetRawMetrics(ctx context.Context, tenantID, yearMonth string) ([]*RawAIQMetrics, error) {
	rows, err := s.pool.Query(ctx, `
		WITH conv AS (
			SELECT user_id,
				conversation_id,
				SUM(completion_tokens)::float / NULLIF(SUM(prompt_tokens),0) AS ratio,
				COUNT(*) AS turns,
				SUM(prompt_tokens + completion_tokens) AS total_tokens
			FROM usage_records
			WHERE tenant_id=$1
				AND to_char(request_at,'YYYY-MM')=$2
				AND conversation_id IS NOT NULL
			GROUP BY user_id, conversation_id
			HAVING COUNT(*) <= 50
		),
		conv_agg AS (
			SELECT user_id,
				PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY ratio) AS od_median,
				PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY turns) AS td_turns_median,
				PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY total_tokens) AS td_tokens_median
			FROM conv GROUP BY user_id
		),
		act AS (
			SELECT user_id,
				COUNT(DISTINCT DATE(request_at)) AS active_days,
				COALESCE(SUM(completion_tokens),0)::float / NULLIF(SUM(scu_cost),0) AS cost_eff
			FROM usage_records
			WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2
			GROUP BY user_id
		)
		SELECT a.user_id,
			COALESCE(c.od_median, 0),
			a.active_days,
			COALESCE(a.cost_eff, 0),
			COALESCE(c.td_turns_median * LN(COALESCE(c.td_tokens_median,0)+1), 0)
		FROM act a LEFT JOIN conv_agg c ON a.user_id=c.user_id`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wdays := WorkingDaysInMonth(yearMonth)

	var result []*RawAIQMetrics
	for rows.Next() {
		m := &RawAIQMetrics{}
		if err := rows.Scan(&m.UserID, &m.OutputDensity, &m.ActiveDays, &m.CostEfficiency, &m.TaskDepth); err != nil {
			return nil, err
		}
		if wdays > 0 {
			m.UsageConsistency = float64(m.ActiveDays) / float64(wdays)
		}
		result = append(result, m)
	}
	return result, nil
}

const (
	MinActiveDays    = 8 // minimum active days for AIQ eligibility
	MinPeerGroupSize = 5 // minimum eligible users for isolated Z-score; smaller groups use global pool
)

// ComputeAIQScores takes raw metrics for users in one peer group and returns
// a map[userID]aiqScore (0–100). Users below MinActiveDays threshold get -1.
func ComputeAIQScores(metrics []*RawAIQMetrics) map[string]float64 {

	eligible := make([]*RawAIQMetrics, 0, len(metrics))
	for _, m := range metrics {
		if m.ActiveDays >= MinActiveDays {
			eligible = append(eligible, m)
		}
	}

	result := make(map[string]float64, len(metrics))
	for _, m := range metrics {
		result[m.UserID] = -1 // below threshold default
	}
	if len(eligible) == 0 {
		return result
	}

	// Cap OutputDensity at 95th percentile (anti-gaming)
	odVals := make([]float64, len(eligible))
	for i, m := range eligible { odVals[i] = m.OutputDensity }
	sort.Float64s(odVals)
	p95idx := int(math.Ceil(0.95*float64(len(odVals)))) - 1
	if p95idx < 0 { p95idx = 0 }
	odCap := odVals[p95idx]

	// Extract raw values for each dimension
	ods  := make([]float64, len(eligible))
	ucs  := make([]float64, len(eligible))
	tds  := make([]float64, len(eligible))
	ces  := make([]float64, len(eligible))
	for i, m := range eligible {
		ods[i] = math.Min(m.OutputDensity, odCap)
		ucs[i] = m.UsageConsistency
		tds[i] = m.TaskDepth
		ces[i] = m.CostEfficiency
	}

	// Z-score normalize each dimension
	zodS := ZScoreNormalize(ods)
	zucS := ZScoreNormalize(ucs)
	ztdS := ZScoreNormalize(tds)
	zceS := ZScoreNormalize(ces)

	for i, m := range eligible {
		aiq := 0.30*ZToScore(zodS[i]) + 0.30*ZToScore(zucS[i]) +
			0.25*ZToScore(ztdS[i]) + 0.15*ZToScore(zceS[i])
		result[m.UserID] = aiq
	}
	return result
}

// --- helpers (exported for tests) ---

func Median(vals []float64) float64 {
	if len(vals) == 0 { return 0 }
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 0 { return (cp[n/2-1] + cp[n/2]) / 2 }
	return cp[n/2]
}

func ZScoreNormalize(vals []float64) []float64 {
	if len(vals) == 0 { return nil }
	var sum float64
	for _, v := range vals { sum += v }
	mean := sum / float64(len(vals))
	var variance float64
	for _, v := range vals { variance += (v - mean) * (v - mean) }
	if len(vals) > 1 { variance /= float64(len(vals) - 1) }
	std := math.Sqrt(variance)
	out := make([]float64, len(vals))
	for i, v := range vals {
		if std > 0 { out[i] = (v - mean) / std } else { out[i] = 0 }
	}
	return out
}

// ZToScore maps a Z-score (clamped to [-3,3]) to [0,100].
func ZToScore(z float64) float64 {
	z = math.Max(-3, math.Min(3, z))
	return (z + 3) / 6 * 100
}

// WorkingDaysInMonth returns the number of Mon–Fri days in yearMonth ("2026-05").
func WorkingDaysInMonth(yearMonth string) int {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil { return 22 } // safe fallback
	year, month := t.Year(), t.Month()
	count := 0
	for d := 1; d <= 31; d++ {
		day := time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
		if day.Month() != month { break }
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			count++
		}
	}
	return count
}
