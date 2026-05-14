# AI Cost Optimization Platform — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将现有 usage_records 数据转化为可操作的成本情报：实时成本看板（按模型/部门/时段）、浪费识别（重复请求、非工作时段异常、模型超配）、预算告警推送。

**Architecture:** `CostAnalysisService` 直接查询 `usage_records`（已有 scu_cost、usd_cost、model 字段）。浪费检测是纯查询逻辑，不需要新表。预算告警通过已有 `BotService` 推送。Dashboard 新增 CostCenterPage，复用现有 TanStack Query 模式。

**Tech Stack:** Go 1.26 + pgx/v5，React 19 + TanStack Query v5 + Tailwind v4

**Spec:** `docs/superpowers/plans/2026-05-14-product-strategy.md`

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `admin/services/cost_analysis.go` | Create | CostAnalysisService：成本分解、浪费检测、优化建议 |
| `admin/services/cost_analysis_test.go` | Create | 测试纯函数：ClassifyWaste、FormatOptimizationSuggestion |
| `admin/api/cost_analysis.go` | Create | REST 路由：breakdown / waste / alerts / suggestions |
| `admin/api/cost_analysis_test.go` | Create | HTTP handler 测试 |
| `dashboard/src/pages/admin/CostCenterPage.tsx` | Create | 成本中心：实时看板 + 浪费报告 + 优化建议 |

无需新 migration——所有数据已在 `usage_records`、`users`、`model_configs` 表中。

---

## Task 1: CostAnalysisService — 成本分解

**Files:**
- Create: `admin/services/cost_analysis.go`
- Create: `admin/services/cost_analysis_test.go`

- [ ] **Step 1: 写失败的测试（纯函数部分）**

创建 `admin/services/cost_analysis_test.go`：

```go
package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestClassifyModelTier_Cheap(t *testing.T) {
	tier := services.ClassifyModelTier("gpt-4o-mini")
	assert.Equal(t, "cheap", tier)
}

func TestClassifyModelTier_Standard(t *testing.T) {
	tier := services.ClassifyModelTier("gpt-4o")
	assert.Equal(t, "standard", tier)
}

func TestClassifyModelTier_Premium(t *testing.T) {
	tier := services.ClassifyModelTier("claude-opus-4-7")
	assert.Equal(t, "premium", tier)
}

func TestClassifyModelTier_Unknown(t *testing.T) {
	tier := services.ClassifyModelTier("some-unknown-model")
	assert.Equal(t, "standard", tier) // default to standard if unknown
}

func TestIsOffHours_Weekday_Night(t *testing.T) {
	// Tuesday 03:00 UTC → off-hours
	ts := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	assert.True(t, services.IsOffHours(ts))
}

func TestIsOffHours_Weekday_WorkHours(t *testing.T) {
	// Tuesday 10:00 UTC → work hours
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	assert.False(t, services.IsOffHours(ts))
}

func TestIsOffHours_Weekend(t *testing.T) {
	// Saturday 10:00 UTC → off-hours (weekend)
	ts := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	assert.True(t, services.IsOffHours(ts))
}

func TestWasteSavingsEstimate_OverspecifiedModel(t *testing.T) {
	// gpt-4o used for simple task (output tokens < 200) → could use cheap model
	savings := services.WasteSavingsEstimate("gpt-4o", 1.50, "overspecified_model")
	// Cheap model is ~10x cheaper → savings ≈ 90%
	assert.InDelta(t, 1.35, savings, 0.05)
}

func TestWasteSavingsEstimate_DuplicateRequest(t *testing.T) {
	savings := services.WasteSavingsEstimate("gpt-4o-mini", 0.10, "duplicate_request")
	// Duplicate → 100% savings (should be cached)
	assert.InDelta(t, 0.10, savings, 0.001)
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestClassifyModel -v 2>&1 | head -10
```

Expected: FAIL — `services.ClassifyModelTier undefined`

- [ ] **Step 3: 实现 cost_analysis.go（第一部分：类型 + 纯函数）**

创建 `admin/services/cost_analysis.go`：

```go
package services

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// CostBreakdown is the top-level cost summary for a time range.
type CostBreakdown struct {
	TotalUSD        float64          `json:"total_usd"`
	TotalSCU        float64          `json:"total_scu"`
	ByModel         []ModelCostItem  `json:"by_model"`
	ByDepartment    []DeptCostItem   `json:"by_department"`
	ByHour          []HourlyCostItem `json:"by_hour"`
}

type ModelCostItem struct {
	Model      string  `json:"model"`
	ModelTier  string  `json:"model_tier"`
	TotalUSD   float64 `json:"total_usd"`
	TotalSCU   float64 `json:"total_scu"`
	RequestCount int64 `json:"request_count"`
}

type DeptCostItem struct {
	Department   string  `json:"department"`
	TotalUSD     float64 `json:"total_usd"`
	TotalSCU     float64 `json:"total_scu"`
	RequestCount int64   `json:"request_count"`
	UserCount    int     `json:"user_count"`
}

type HourlyCostItem struct {
	Hour       int     `json:"hour"`       // 0–23 UTC
	TotalUSD   float64 `json:"total_usd"`
	IsOffHours bool    `json:"is_off_hours"`
}

// WasteItem is one detected waste pattern.
type WasteItem struct {
	WasteType        string  `json:"waste_type"`         // overspecified_model | off_hours_spike | duplicate_pattern
	Description      string  `json:"description"`
	EstimatedSavings float64 `json:"estimated_savings_usd"`
	AffectedRequests int64   `json:"affected_requests"`
}

// OptimizationSuggestion is an actionable recommendation.
type OptimizationSuggestion struct {
	Priority    string  `json:"priority"`  // high | medium | low
	Action      string  `json:"action"`
	Detail      string  `json:"detail"`
	SavingsUSD  float64 `json:"savings_usd"`
}

// ─── Pure helpers (exported for testing) ─────────────────────────────────────

var cheapModels = map[string]bool{
	"gpt-4o-mini": true, "claude-haiku-4-5-20251001": true,
	"claude-haiku-4-5": true, "gemini-flash": true,
}

var premiumModels = map[string]bool{
	"claude-opus-4-7": true, "claude-opus-4-5": true,
	"gpt-4-turbo": true, "o1": true, "o1-preview": true,
}

// ClassifyModelTier returns "cheap", "standard", or "premium" for a model name.
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

// IsOffHours returns true for weekends or UTC hours outside 06:00–22:00.
func IsOffHours(ts time.Time) bool {
	wd := ts.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return true
	}
	return ts.Hour() < 6 || ts.Hour() >= 22
}

// WasteSavingsEstimate returns the USD savings achievable for one waste item.
func WasteSavingsEstimate(model string, costUSD float64, wasteType string) float64 {
	switch wasteType {
	case "duplicate_request":
		return costUSD // 100% savings via caching
	case "overspecified_model":
		// Cheap model is ~10x cheaper → switching saves ~90%
		return costUSD * 0.90
	case "off_hours_spike":
		return costUSD * 0.50 // estimate: half of off-hours spend is non-work
	default:
		return 0
	}
}

// ─── Service ──────────────────────────────────────────────────────────────────

type CostAnalysisService struct {
	pool *pgxpool.Pool
}

func NewCostAnalysisService(pool *pgxpool.Pool) *CostAnalysisService {
	return &CostAnalysisService{pool: pool}
}
```

- [ ] **Step 4: 运行纯函数测试，确认通过**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestClassifyModel|TestIsOffHours|TestWasteSavings" -v 2>&1
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add admin/services/cost_analysis.go admin/services/cost_analysis_test.go
git commit -m "feat(cost): CostAnalysisService skeleton — types + pure helpers + tests"
```

---

## Task 2: CostAnalysisService — DB 查询方法

**Files:**
- Modify: `admin/services/cost_analysis.go`（追加 DB 方法）

- [ ] **Step 1: 追加 GetCostBreakdown 到 cost_analysis.go**

在文件末尾添加：

```go
// GetCostBreakdown returns cost totals broken down by model, department, and hour.
func (s *CostAnalysisService) GetCostBreakdown(ctx context.Context, tenantID, yearMonth string) (*CostBreakdown, error) {
	result := &CostBreakdown{}

	// Total
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(usd_cost),0), COALESCE(SUM(scu_cost),0)
		 FROM usage_records
		 WHERE tenant_id=$1 AND to_char(created_at,'YYYY-MM')=$2`,
		tenantID, yearMonth).Scan(&result.TotalUSD, &result.TotalSCU)
	if err != nil {
		return nil, err
	}

	// By model
	rows, err := s.pool.Query(ctx,
		`SELECT model,
		        COALESCE(SUM(usd_cost),0), COALESCE(SUM(scu_cost),0), COUNT(*)
		 FROM usage_records
		 WHERE tenant_id=$1 AND to_char(created_at,'YYYY-MM')=$2
		 GROUP BY model ORDER BY SUM(usd_cost) DESC`,
		tenantID, yearMonth)
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

	// By department
	deptRows, err := s.pool.Query(ctx,
		`SELECT COALESCE(u.department,'(未设置)'),
		        COALESCE(SUM(r.usd_cost),0), COALESCE(SUM(r.scu_cost),0),
		        COUNT(r.id), COUNT(DISTINCT r.user_id)
		 FROM usage_records r
		 JOIN users u ON u.id = r.user_id
		 WHERE r.tenant_id=$1 AND to_char(r.created_at,'YYYY-MM')=$2
		 GROUP BY u.department ORDER BY SUM(r.usd_cost) DESC`,
		tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer deptRows.Close()
	for deptRows.Next() {
		var item DeptCostItem
		if err := deptRows.Scan(&item.Department, &item.TotalUSD, &item.TotalSCU,
			&item.RequestCount, &item.UserCount); err != nil {
			return nil, err
		}
		result.ByDepartment = append(result.ByDepartment, item)
	}
	if err := deptRows.Err(); err != nil {
		return nil, err
	}

	// By hour (UTC)
	hourRows, err := s.pool.Query(ctx,
		`SELECT EXTRACT(HOUR FROM created_at)::int AS h,
		        COALESCE(SUM(usd_cost),0)
		 FROM usage_records
		 WHERE tenant_id=$1 AND to_char(created_at,'YYYY-MM')=$2
		 GROUP BY h ORDER BY h`,
		tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer hourRows.Close()
	for hourRows.Next() {
		var item HourlyCostItem
		var h int
		if err := hourRows.Scan(&h, &item.TotalUSD); err != nil {
			return nil, err
		}
		item.Hour = h
		item.IsOffHours = h < 6 || h >= 22
		result.ByHour = append(result.ByHour, item)
	}
	return result, hourRows.Err()
}

// GetWasteReport detects cost waste patterns for the given month.
func (s *CostAnalysisService) GetWasteReport(ctx context.Context, tenantID, yearMonth string) ([]*WasteItem, error) {
	var items []*WasteItem

	// 1. Overspecified model: premium/standard model, output_tokens < 200
	var overspecCount int64
	var overspecCost float64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(usd_cost),0)
		 FROM usage_records
		 WHERE tenant_id=$1 AND to_char(created_at,'YYYY-MM')=$2
		   AND output_tokens < 200
		   AND model NOT ILIKE '%mini%'
		   AND model NOT ILIKE '%haiku%'
		   AND model NOT ILIKE '%flash%'`,
		tenantID, yearMonth).Scan(&overspecCount, &overspecCost)
	if err != nil {
		return nil, err
	}
	if overspecCount > 0 {
		items = append(items, &WasteItem{
			WasteType:        "overspecified_model",
			Description:      "使用高级模型处理简单任务（输出 < 200 tokens）",
			EstimatedSavings: WasteSavingsEstimate("", overspecCost, "overspecified_model"),
			AffectedRequests: overspecCount,
		})
	}

	// 2. Off-hours spike: cost between 22:00–06:00 UTC or weekends
	var offHoursCost float64
	var offHoursCount int64
	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(usd_cost),0)
		 FROM usage_records
		 WHERE tenant_id=$1 AND to_char(created_at,'YYYY-MM')=$2
		   AND (EXTRACT(HOUR FROM created_at) < 6
		        OR EXTRACT(HOUR FROM created_at) >= 22
		        OR EXTRACT(DOW FROM created_at) IN (0,6))`,
		tenantID, yearMonth).Scan(&offHoursCount, &offHoursCost)
	if err != nil {
		return nil, err
	}
	if offHoursCost > 5.0 { // only flag if meaningful amount
		items = append(items, &WasteItem{
			WasteType:        "off_hours_spike",
			Description:      "非工作时段（22:00–06:00 UTC 或周末）AI 消耗异常",
			EstimatedSavings: WasteSavingsEstimate("", offHoursCost, "off_hours_spike"),
			AffectedRequests: offHoursCount,
		})
	}

	return items, nil
}

// GetOptimizationSuggestions converts waste items to ranked action suggestions.
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
```

- [ ] **Step 2: 运行全量 admin 测试**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -count=1 2>&1
```

Expected: `ok admin/services`, 0 failures.

- [ ] **Step 3: Commit**

```bash
git add admin/services/cost_analysis.go
git commit -m "feat(cost): CostAnalysisService — GetCostBreakdown + GetWasteReport + GetOptimizationSuggestions"
```

---

## Task 3: API 路由

**Files:**
- Create: `admin/api/cost_analysis.go`
- Modify: 路由注册文件（`admin/api/middleware.go` 或 `admin/main.go`）

- [ ] **Step 1: 创建 cost_analysis.go**

```go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type CostAnalysisHandler struct {
	svc    *services.CostAnalysisService
	botSvc *services.BotService
}

func NewCostAnalysisHandler(svc *services.CostAnalysisService, botSvc *services.BotService) *CostAnalysisHandler {
	return &CostAnalysisHandler{svc: svc, botSvc: botSvc}
}

func (h *CostAnalysisHandler) RegisterRoutes(app fiber.Router, jwtMW fiber.Handler) {
	g := app.Group("/api/admin/cost", jwtMW, AdminOnly)
	g.Get("/breakdown", h.getBreakdown)
	g.Get("/waste", h.getWaste)
	g.Get("/suggestions", h.getSuggestions)
	g.Post("/alert-test", h.testAlert)
}

// GET /api/admin/cost/breakdown?month=2026-05
func (h *CostAnalysisHandler) getBreakdown(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	data, err := h.svc.GetCostBreakdown(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

// GET /api/admin/cost/waste?month=2026-05
func (h *CostAnalysisHandler) getWaste(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	data, err := h.svc.GetWasteReport(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

// GET /api/admin/cost/suggestions?month=2026-05
func (h *CostAnalysisHandler) getSuggestions(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	data, err := h.svc.GetOptimizationSuggestions(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

// POST /api/admin/cost/alert-test — 立即发送本月优化摘要到 Bot
func (h *CostAnalysisHandler) testAlert(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := currentYearMonth()
	breakdown, err := h.svc.GetCostBreakdown(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	suggestions, err := h.svc.GetOptimizationSuggestions(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	msg := formatCostAlert(month, breakdown.TotalUSD, suggestions)
	if err := h.botSvc.BroadcastAlert(c.Context(), tenantID, msg); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"sent": true, "message": msg})
}

func formatCostAlert(month string, totalUSD float64, suggestions []*services.OptimizationSuggestion) string {
	msg := fmt.Sprintf("💰 %s AI 成本报告\n总支出: $%.2f\n\n优化建议:\n", month, totalUSD)
	for i, s := range suggestions {
		if i >= 3 {
			break
		}
		msg += fmt.Sprintf("• [%s] %s（可节省 $%.2f）\n", s.Priority, s.Action, s.SavingsUSD)
	}
	return msg
}
```

> 需在文件顶部加 `import "fmt"`。

- [ ] **Step 2: 在路由注册文件中加入 CostAnalysisHandler**

```go
costSvc := services.NewCostAnalysisService(pool)
costHandler := api.NewCostAnalysisHandler(costSvc, botSvc)
costHandler.RegisterRoutes(app, jwtMiddleware)
```

- [ ] **Step 3: 运行全量测试**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... -count=1 2>&1
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add admin/api/cost_analysis.go
git commit -m "feat(cost): cost analysis API routes — breakdown, waste, suggestions, alert"
```

---

## Task 4: Dashboard CostCenterPage

**Files:**
- Create: `dashboard/src/pages/admin/CostCenterPage.tsx`
- Modify: `dashboard/src/App.tsx`（路由）+ 侧边栏

- [ ] **Step 1: 创建 CostCenterPage.tsx**

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

const API = '/api/admin/cost'

interface ModelCostItem {
  model: string
  model_tier: 'cheap' | 'standard' | 'premium'
  total_usd: number
  total_scu: number
  request_count: number
}

interface DeptCostItem {
  department: string
  total_usd: number
  request_count: number
  user_count: number
}

interface HourlyCostItem {
  hour: number
  total_usd: number
  is_off_hours: boolean
}

interface CostBreakdown {
  total_usd: number
  total_scu: number
  by_model: ModelCostItem[]
  by_department: DeptCostItem[]
  by_hour: HourlyCostItem[]
}

interface WasteItem {
  waste_type: string
  description: string
  estimated_savings_usd: number
  affected_requests: number
}

interface Suggestion {
  priority: 'high' | 'medium' | 'low'
  action: string
  detail: string
  savings_usd: number
}

const tierColors: Record<string, string> = {
  cheap: 'bg-green-100 text-green-700',
  standard: 'bg-blue-100 text-blue-700',
  premium: 'bg-purple-100 text-purple-700',
}

const priorityColors: Record<string, string> = {
  high: 'bg-red-100 text-red-700',
  medium: 'bg-yellow-100 text-yellow-700',
  low: 'bg-gray-100 text-gray-700',
}

export default function CostCenterPage() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))

  const { data: breakdown } = useQuery<CostBreakdown>({
    queryKey: ['cost-breakdown', month],
    queryFn: () => fetch(`${API}/breakdown?month=${month}`).then(r => r.json()),
  })

  const { data: waste = [] } = useQuery<WasteItem[]>({
    queryKey: ['cost-waste', month],
    queryFn: () => fetch(`${API}/waste?month=${month}`).then(r => r.json()),
  })

  const { data: suggestions = [] } = useQuery<Suggestion[]>({
    queryKey: ['cost-suggestions', month],
    queryFn: () => fetch(`${API}/suggestions?month=${month}`).then(r => r.json()),
  })

  const totalSavings = waste.reduce((sum, w) => sum + w.estimated_savings_usd, 0)

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">成本中心</h1>
        <input
          type="month"
          value={month}
          onChange={e => setMonth(e.target.value)}
          className="border rounded px-3 py-1 text-sm"
        />
      </div>

      {/* 顶部统计卡 */}
      <div className="grid grid-cols-3 gap-4">
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">本月 AI 总支出</p>
          <p className="text-3xl font-bold">${breakdown?.total_usd?.toFixed(2) ?? '0.00'}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">可节省估算</p>
          <p className="text-3xl font-bold text-green-600">${totalSavings.toFixed(2)}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">优化建议数</p>
          <p className="text-3xl font-bold text-blue-600">{suggestions.length}</p>
        </div>
      </div>

      {/* 优化建议 */}
      {suggestions.length > 0 && (
        <div className="bg-white border rounded-xl p-4">
          <h2 className="font-semibold mb-3">优化建议</h2>
          <div className="space-y-3">
            {suggestions.map((s, i) => (
              <div key={i} className="flex items-start gap-3 p-3 bg-gray-50 rounded-lg">
                <span className={`px-2 py-0.5 rounded text-xs font-medium shrink-0 ${priorityColors[s.priority]}`}>
                  {s.priority}
                </span>
                <div className="flex-1">
                  <p className="font-medium text-sm">{s.action}</p>
                  <p className="text-xs text-gray-500 mt-0.5">{s.detail}</p>
                </div>
                <span className="text-green-600 font-mono text-sm shrink-0">
                  节省 ${s.savings_usd.toFixed(2)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 模型成本明细 */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">模型成本分布</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['模型', '级别', '支出 (USD)', 'SCU', '请求数'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(breakdown?.by_model ?? []).map((m, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2 font-mono text-xs">{m.model}</td>
                <td className="px-3 py-2">
                  <span className={`px-2 py-0.5 rounded text-xs ${tierColors[m.model_tier]}`}>
                    {m.model_tier}
                  </span>
                </td>
                <td className="px-3 py-2 font-mono">${m.total_usd.toFixed(3)}</td>
                <td className="px-3 py-2 font-mono">{m.total_scu.toFixed(1)}</td>
                <td className="px-3 py-2">{m.request_count.toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* 部门成本分布 */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">部门成本分布</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['部门', '支出 (USD)', '请求数', '员工数'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(breakdown?.by_department ?? []).map((d, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2">{d.department}</td>
                <td className="px-3 py-2 font-mono">${d.total_usd.toFixed(3)}</td>
                <td className="px-3 py-2">{d.request_count.toLocaleString()}</td>
                <td className="px-3 py-2">{d.user_count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* 浪费检测 */}
      {waste.length > 0 && (
        <div className="bg-white border rounded-xl p-4">
          <h2 className="font-semibold mb-3">浪费检测</h2>
          <div className="space-y-2">
            {waste.map((w, i) => (
              <div key={i} className="flex items-center justify-between p-3 bg-orange-50 border border-orange-100 rounded-lg">
                <div>
                  <p className="text-sm font-medium">{w.description}</p>
                  <p className="text-xs text-gray-500">影响请求数: {w.affected_requests.toLocaleString()}</p>
                </div>
                <span className="text-orange-600 font-mono font-medium text-sm">
                  浪费 ${w.estimated_savings_usd.toFixed(2)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: 在路由中注册**

```tsx
import CostCenterPage from './pages/admin/CostCenterPage'
// 在 admin 路由组中：
<Route path="/cost" element={<CostCenterPage />} />
```

- [ ] **Step 3: 在侧边栏加入入口**

```tsx
{ path: '/cost', label: '成本中心', icon: CurrencyDollarIcon }
```

- [ ] **Step 4: 启动前端验证**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run dev
```

以 admin 身份登录，确认"成本中心"页面出现，统计卡显示数据，无报错。

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/admin/CostCenterPage.tsx dashboard/src/App.tsx
git commit -m "feat(cost): CostCenterPage — cost breakdown by model/dept, waste detection, suggestions"
```

---

## 验证清单

```bash
# 1. 全量 Go 测试
cd /Users/sugac.275/ToTra/admin && go test ./... -count=1

# 2. 成本分解 API
curl http://localhost:8081/api/admin/cost/breakdown?month=2026-05 \
  -H "Authorization: Bearer <admin-token>"
# Expected: { total_usd, by_model: [...], by_department: [...], by_hour: [...] }

# 3. 浪费检测
curl http://localhost:8081/api/admin/cost/waste?month=2026-05 \
  -H "Authorization: Bearer <admin-token>"
# Expected: 列出 overspecified_model 等浪费项

# 4. 优化建议
curl http://localhost:8081/api/admin/cost/suggestions?month=2026-05 \
  -H "Authorization: Bearer <admin-token>"
# Expected: 带 priority 和 savings_usd 的建议列表

# 5. Bot 测试告警
curl -X POST http://localhost:8081/api/admin/cost/alert-test \
  -H "Authorization: Bearer <admin-token>"
# Expected: { sent: true, message: "💰 2026-05 AI 成本报告..." }
```
