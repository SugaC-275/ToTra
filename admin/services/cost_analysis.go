package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---- Types ----

type CostBreakdown struct {
	TotalUSD     float64          `json:"total_usd"`
	TotalSCU     float64          `json:"total_scu"`
	ByModel      []ModelCostItem  `json:"by_model"`
	ByDepartment []DeptCostItem   `json:"by_department"`
	ByHour       []HourlyCostItem `json:"by_hour"`
}

type ModelCostItem struct {
	Model        string  `json:"model"`
	ModelTier    string  `json:"model_tier"`
	TotalUSD     float64 `json:"total_usd"`
	TotalSCU     float64 `json:"total_scu"`
	RequestCount int64   `json:"request_count"`
}

type DeptCostItem struct {
	Department   string  `json:"department"`
	TotalUSD     float64 `json:"total_usd"`
	TotalSCU     float64 `json:"total_scu"`
	RequestCount int64   `json:"request_count"`
	UserCount    int     `json:"user_count"`
}

type HourlyCostItem struct {
	Hour       int     `json:"hour"`
	TotalUSD   float64 `json:"total_usd"`
	IsOffHours bool    `json:"is_off_hours"`
}

type WasteItem struct {
	WasteType        string  `json:"waste_type"`
	Description      string  `json:"description"`
	EstimatedSavings float64 `json:"estimated_savings_usd"`
	AffectedRequests int64   `json:"affected_requests"`
}

type OptimizationSuggestion struct {
	Priority   string  `json:"priority"`
	Action     string  `json:"action"`
	Detail     string  `json:"detail"`
	SavingsUSD float64 `json:"savings_usd"`
}

// ---- Pure helpers ----

var cheapModels = map[string]bool{
	"gpt-4o-mini":              true,
	"claude-haiku-4-5-20251001": true,
	"claude-haiku-4-5":          true,
	"gemini-flash":              true,
}

var premiumModels = map[string]bool{
	"claude-opus-4-7":  true,
	"claude-opus-4-5":  true,
	"gpt-4-turbo":      true,
	"o1":               true,
	"o1-preview":       true,
}

func ClassifyModelTier(model string) string {
	lower := strings.ToLower(model)
	for m := range cheapModels {
		if strings.Contains(lower, m) {
			return "cheap"
		}
	}
	for m := range premiumModels {
		if strings.Contains(lower, m) {
			return "premium"
		}
	}
	return "standard"
}

func IsOffHours(ts time.Time) bool {
	wd := ts.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return true
	}
	return ts.Hour() < 6 || ts.Hour() >= 22
}

func WasteSavingsEstimate(model string, costUSD float64, wasteType string) float64 {
	switch wasteType {
	case "duplicate_request":
		return costUSD
	case "overspecified_model":
		return costUSD * 0.90
	case "off_hours_spike":
		return costUSD * 0.50
	default:
		return 0
	}
}

// ---- Service ----

type CostAnalysisService struct {
	pool *pgxpool.Pool
}

func NewCostAnalysisService(pool *pgxpool.Pool) *CostAnalysisService {
	return &CostAnalysisService{pool: pool}
}

func costCurrentYearMonth() string {
	return time.Now().UTC().Format("2006-01")
}

// GetCostBreakdown returns cost breakdown for the given tenant and year-month (e.g. "2026-05").
// usage_records schema: tenant_id, user_id, model_config_id, scu_cost, usd_cost,
//   completion_tokens, request_at
// model_configs schema: id, tenant_id, name
// users schema: id, tenant_id, department
func (s *CostAnalysisService) GetCostBreakdown(ctx context.Context, tenantID, yearMonth string) (*CostBreakdown, error) {
	result := &CostBreakdown{}

	// Total USD + SCU
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0), COALESCE(SUM(scu_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at, 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&result.TotalUSD, &result.TotalSCU)
	if err != nil {
		return nil, err
	}

	// By model: join model_configs to get the model name
	rows, err := s.pool.Query(ctx, `
		SELECT mc.name,
		       COALESCE(SUM(ur.usd_cost), 0) AS total_usd,
		       COALESCE(SUM(ur.scu_cost), 0) AS total_scu,
		       COUNT(*) AS request_count
		FROM usage_records ur
		JOIN model_configs mc ON mc.id = ur.model_config_id
		WHERE ur.tenant_id = $1
		  AND to_char(ur.request_at, 'YYYY-MM') = $2
		GROUP BY mc.name
		ORDER BY total_usd DESC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item ModelCostItem
		if err := rows.Scan(&item.Model, &item.TotalUSD, &item.TotalSCU, &item.RequestCount); err != nil {
			return nil, err
		}
		item.ModelTier = ClassifyModelTier(item.Model)
		result.ByModel = append(result.ByModel, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// By department: join users
	deptRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(u.department, '(未设置)') AS dept,
		       COALESCE(SUM(ur.usd_cost), 0)      AS total_usd,
		       COALESCE(SUM(ur.scu_cost), 0)      AS total_scu,
		       COUNT(*)                            AS request_count,
		       COUNT(DISTINCT ur.user_id)          AS user_count
		FROM usage_records ur
		JOIN users u ON u.id = ur.user_id
		WHERE ur.tenant_id = $1
		  AND to_char(ur.request_at, 'YYYY-MM') = $2
		GROUP BY dept
		ORDER BY total_usd DESC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer deptRows.Close()

	for deptRows.Next() {
		var item DeptCostItem
		if err := deptRows.Scan(&item.Department, &item.TotalUSD, &item.TotalSCU, &item.RequestCount, &item.UserCount); err != nil {
			return nil, err
		}
		result.ByDepartment = append(result.ByDepartment, item)
	}
	if err := deptRows.Err(); err != nil {
		return nil, err
	}

	// By hour
	hourRows, err := s.pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM request_at)::int AS hr,
		       COALESCE(SUM(usd_cost), 0)         AS total_usd
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at, 'YYYY-MM') = $2
		GROUP BY hr
		ORDER BY hr
	`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer hourRows.Close()

	for hourRows.Next() {
		var item HourlyCostItem
		if err := hourRows.Scan(&item.Hour, &item.TotalUSD); err != nil {
			return nil, err
		}
		// Use a synthetic timestamp at that hour on a weekday to check off-hours
		item.IsOffHours = IsOffHours(time.Date(2006, 1, 2, item.Hour, 0, 0, 0, time.UTC))
		result.ByHour = append(result.ByHour, item)
	}
	if err := hourRows.Err(); err != nil {
		return nil, err
	}

	if result.ByModel == nil {
		result.ByModel = []ModelCostItem{}
	}
	if result.ByDepartment == nil {
		result.ByDepartment = []DeptCostItem{}
	}
	if result.ByHour == nil {
		result.ByHour = []HourlyCostItem{}
	}

	return result, nil
}

// GetWasteReport detects waste patterns for a given tenant + month.
// Waste types:
//   - overspecified_model: premium/standard model used but completion_tokens < 200
//   - off_hours_spike: requests outside 06:00-22:00 UTC or weekends with usd_cost > 5
func (s *CostAnalysisService) GetWasteReport(ctx context.Context, tenantID, yearMonth string) ([]*WasteItem, error) {
	var items []*WasteItem

	// Overspecified model: premium or standard model with completion_tokens < 200
	overRows, err := s.pool.Query(ctx, `
		SELECT mc.name,
		       COUNT(*)             AS cnt,
		       COALESCE(SUM(ur.usd_cost), 0) AS total_cost
		FROM usage_records ur
		JOIN model_configs mc ON mc.id = ur.model_config_id
		WHERE ur.tenant_id = $1
		  AND to_char(ur.request_at, 'YYYY-MM') = $2
		  AND ur.completion_tokens < 200
		GROUP BY mc.name
		HAVING COUNT(*) > 0
		ORDER BY total_cost DESC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer overRows.Close()

	for overRows.Next() {
		var modelName string
		var cnt int64
		var totalCost float64
		if err := overRows.Scan(&modelName, &cnt, &totalCost); err != nil {
			return nil, err
		}
		tier := ClassifyModelTier(modelName)
		// Only flag premium and standard models used for short completions
		if tier == "cheap" {
			continue
		}
		savings := WasteSavingsEstimate(modelName, totalCost, "overspecified_model")
		if savings <= 0 {
			continue
		}
		items = append(items, &WasteItem{
			WasteType:        "overspecified_model",
			Description:      modelName + " 被用于短回复（<200 tokens），建议切换到低成本模型",
			EstimatedSavings: savings,
			AffectedRequests: cnt,
		})
	}
	if err := overRows.Err(); err != nil {
		return nil, err
	}

	// Off-hours spike: requests outside 06:00-22:00 UTC or weekends with high daily cost
	offRows, err := s.pool.Query(ctx, `
		SELECT COUNT(*) AS cnt,
		       COALESCE(SUM(usd_cost), 0) AS total_cost
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at, 'YYYY-MM') = $2
		  AND (
		    EXTRACT(DOW FROM request_at) IN (0, 6)
		    OR EXTRACT(HOUR FROM request_at) < 6
		    OR EXTRACT(HOUR FROM request_at) >= 22
		  )
	`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer offRows.Close()

	for offRows.Next() {
		var cnt int64
		var totalCost float64
		if err := offRows.Scan(&cnt, &totalCost); err != nil {
			return nil, err
		}
		if totalCost > 5 {
			savings := WasteSavingsEstimate("", totalCost, "off_hours_spike")
			items = append(items, &WasteItem{
				WasteType:        "off_hours_spike",
				Description:      fmt.Sprintf("非工作时段（周末/深夜）AI 调用，总成本 $%.2f", totalCost),
				EstimatedSavings: savings,
				AffectedRequests: cnt,
			})
		}
	}
	if err := offRows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// GetOptimizationSuggestions derives suggestions from the waste report.
func (s *CostAnalysisService) GetOptimizationSuggestions(ctx context.Context, tenantID, yearMonth string) ([]*OptimizationSuggestion, error) {
	waste, err := s.GetWasteReport(ctx, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	var suggestions []*OptimizationSuggestion
	for _, w := range waste {
		priority := "medium"
		if w.EstimatedSavings > 100 {
			priority = "high"
		}
		suggestions = append(suggestions, &OptimizationSuggestion{
			Priority:   priority,
			Action:     describeAction(w.WasteType),
			Detail:     w.Description,
			SavingsUSD: w.EstimatedSavings,
		})
	}
	return suggestions, nil
}

func describeAction(wasteType string) string {
	switch wasteType {
	case "overspecified_model":
		return "将简单查询切换到 GPT-4o-mini / Claude Haiku"
	case "off_hours_spike":
		return "排查非工作时段的 AI 使用来源"
	case "duplicate_request":
		return "启用语义缓存减少重复调用"
	default:
		return "审查该类型消耗"
	}
}
