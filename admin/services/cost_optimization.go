package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type OptSuggestion struct {
	Category            string  `json:"category"`
	Title               string  `json:"title"`
	Description         string  `json:"description"`
	EstimatedSavingsUSD float64 `json:"estimated_savings_usd"`
	Priority            string  `json:"priority"` // "high" | "medium" | "low"
}

type OptimizationReport struct {
	TenantID    string           `json:"tenant_id"`
	YearMonth   string           `json:"year_month"`
	Suggestions []*OptSuggestion `json:"suggestions"`
	GeneratedAt string           `json:"generated_at"`
}

// OffHoursSuggestion returns a suggestion when off-hours spend exceeds 15%.
func OffHoursSuggestion(offHoursPercent, offHoursUSD float64) *OptSuggestion {
	if offHoursPercent < 15 {
		return nil
	}
	priority := "medium"
	if offHoursPercent > 30 {
		priority = "high"
	}
	return &OptSuggestion{
		Category:            "scheduling",
		Title:               "Restrict off-hours AI usage",
		Description:         fmt.Sprintf("%.1f%% of your monthly spend ($%.2f) occurs outside business hours. Enforcing off-hours quotas could reduce waste.", offHoursPercent, offHoursUSD),
		EstimatedSavingsUSD: offHoursUSD * 0.5,
		Priority:            priority,
	}
}

// TopSpenderSuggestion returns a suggestion when one user exceeds 25% of total spend.
func TopSpenderSuggestion(topUserPercent float64) *OptSuggestion {
	if topUserPercent < 25 {
		return nil
	}
	priority := "medium"
	if topUserPercent > 40 {
		priority = "high"
	}
	return &OptSuggestion{
		Category:            "quota",
		Title:               "Rebalance per-user quotas",
		Description:         fmt.Sprintf("Your top spender accounts for %.1f%% of total AI cost. Setting user-level quotas would distribute spend more evenly.", topUserPercent),
		EstimatedSavingsUSD: 0,
		Priority:            priority,
	}
}

// RoutingSuggestion returns a suggestion when avg complexity is low (expensive models over-used).
func RoutingSuggestion(avgComplexity, routingSavingsUSD float64) *OptSuggestion {
	if avgComplexity <= 0 || avgComplexity >= 45 {
		return nil
	}
	return &OptSuggestion{
		Category:            "routing",
		Title:               "Lower smart-routing complexity threshold",
		Description:         fmt.Sprintf("Average request complexity score is %.0f/100. Lowering the routing threshold would route more requests to cost-efficient models.", avgComplexity),
		EstimatedSavingsUSD: routingSavingsUSD * 0.3,
		Priority:            "medium",
	}
}

// BudgetSuggestion returns a warning suggestion when budget utilisation exceeds 75%.
func BudgetSuggestion(usedPercent float64) *OptSuggestion {
	if usedPercent < 75 {
		return nil
	}
	priority := "medium"
	if usedPercent >= 90 {
		priority = "high"
	}
	return &OptSuggestion{
		Category:            "budget",
		Title:               "Review monthly budget",
		Description:         fmt.Sprintf("You have used %.1f%% of your monthly AI budget. Consider adjusting quotas or upgrading your budget cap.", usedPercent),
		EstimatedSavingsUSD: 0,
		Priority:            priority,
	}
}

// BenchmarkSuggestion returns a suggestion when spend percentile rank exceeds 60.
func BenchmarkSuggestion(percentileRank float64) *OptSuggestion {
	if percentileRank < 60 {
		return nil
	}
	priority := "low"
	if percentileRank >= 80 {
		priority = "medium"
	}
	return &OptSuggestion{
		Category:            "benchmark",
		Title:               "Cost above industry median",
		Description:         fmt.Sprintf("Your per-user AI spend is higher than %.0f%% of comparable tenants. Review model selection and caching policies.", percentileRank),
		EstimatedSavingsUSD: 0,
		Priority:            priority,
	}
}

var priorityOrder = map[string]int{"high": 0, "medium": 1, "low": 2}

// SortSuggestions sorts suggestions high → medium → low, then by estimated savings desc.
func SortSuggestions(s []*OptSuggestion) []*OptSuggestion {
	sort.SliceStable(s, func(i, j int) bool {
		pi, pj := priorityOrder[s[i].Priority], priorityOrder[s[j].Priority]
		if pi != pj {
			return pi < pj
		}
		return s[i].EstimatedSavingsUSD > s[j].EstimatedSavingsUSD
	})
	return s
}

type OptimizationSuggestionService struct{ pool *pgxpool.Pool }

func NewOptimizationSuggestionService(pool *pgxpool.Pool) *OptimizationSuggestionService {
	return &OptimizationSuggestionService{pool: pool}
}

func (s *OptimizationSuggestionService) GetSuggestions(ctx context.Context, tenantID string) (*OptimizationReport, error) {
	yearMonth := time.Now().UTC().Format("2006-01")
	report := &OptimizationReport{
		TenantID:    tenantID,
		YearMonth:   yearMonth,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	var suggestions []*OptSuggestion

	// Off-hours signal
	var totalUSD, offHoursUSD float64
	_ = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(usd_cost), 0),
			COALESCE(SUM(CASE WHEN EXTRACT(HOUR FROM request_at AT TIME ZONE 'UTC') NOT BETWEEN 9 AND 17 THEN usd_cost ELSE 0 END), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&totalUSD, &offHoursUSD)
	if totalUSD > 0 {
		if s := OffHoursSuggestion(offHoursUSD/totalUSD*100, offHoursUSD); s != nil {
			suggestions = append(suggestions, s)
		}
	}

	// Top spender concentration
	var topUserUSD float64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(user_total), 0)
		FROM (
			SELECT user_id, SUM(usd_cost) AS user_total
			FROM usage_records
			WHERE tenant_id = $1
			  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
			GROUP BY user_id
		) sub
	`, tenantID, yearMonth).Scan(&topUserUSD)
	if totalUSD > 0 {
		if s := TopSpenderSuggestion(topUserUSD / totalUSD * 100); s != nil {
			suggestions = append(suggestions, s)
		}
	}

	// Routing signal
	var avgComplexity, routingSavings float64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(complexity_score::float), 0), COALESCE(SUM(usd_saved), 0)
		FROM gateway_routing_events
		WHERE tenant_id = $1
		  AND to_char(routed_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&avgComplexity, &routingSavings)
	if s := RoutingSuggestion(avgComplexity, routingSavings); s != nil {
		suggestions = append(suggestions, s)
	}

	// Budget utilisation
	var spentUSD float64
	var monthlyBudget *float64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(r.usd_cost), 0), cs.monthly_budget_usd
		FROM usage_records r
		LEFT JOIN tenant_cost_settings cs ON cs.tenant_id = r.tenant_id
		WHERE r.tenant_id = $1
		  AND to_char(r.request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY cs.monthly_budget_usd
	`, tenantID, yearMonth).Scan(&spentUSD, &monthlyBudget)
	if monthlyBudget != nil && *monthlyBudget > 0 {
		if s := BudgetSuggestion(spentUSD / *monthlyBudget * 100); s != nil {
			suggestions = append(suggestions, s)
		}
	}

	// Cross-tenant benchmark
	var percentileRank float64
	_ = s.pool.QueryRow(ctx, `
		WITH rates AS (
			SELECT tenant_id, SUM(usd_cost) / NULLIF(COUNT(DISTINCT user_id), 0) AS per_user
			FROM usage_records
			WHERE to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
			GROUP BY tenant_id
		),
		mine AS (SELECT per_user FROM rates WHERE tenant_id = $1)
		SELECT COUNT(*)::float / NULLIF((SELECT COUNT(*) FROM rates), 0) * 100
		FROM rates r, mine m
		WHERE r.per_user < m.per_user
	`, tenantID, yearMonth).Scan(&percentileRank)
	if s := BenchmarkSuggestion(percentileRank); s != nil {
		suggestions = append(suggestions, s)
	}

	if suggestions == nil {
		suggestions = []*OptSuggestion{}
	}
	report.Suggestions = SortSuggestions(suggestions)
	return report, nil
}
