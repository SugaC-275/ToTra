# Cost Optimization Platform Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add budget forecast (linear regression EOY projection), cross-tenant cost benchmark (per-user monthly spend percentiles), and model ROI ranking to the Cost Center.

**Architecture:** Three pure-function services behind three new admin API endpoints; `CostCenterPage.tsx` extended with a Budget Forecast panel and a Benchmark card. No new DB tables — all queries run against existing `usage_records` and `gateway_routing_events`. Cross-tenant queries aggregate percentiles only; no individual tenant data is exposed.

**Tech Stack:** Go 1.26 + pgx/v5 (admin service) · Fiber v2 (admin API) · React 19 + TanStack Query v5 + Tailwind v4 (dashboard)

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `admin/services/budget_forecast.go` | `LinearRegression` pure fn + `BudgetForecastService.GetBudgetForecast` |
| Create | `admin/services/budget_forecast_test.go` | Unit tests for `LinearRegression` |
| Create | `admin/services/cost_benchmark.go` | `CostBenchmarkService.GetCostBenchmark` + `GetModelROI` |
| Create | `admin/services/cost_benchmark_test.go` | Unit tests for percentile helpers |
| Create | `admin/api/cost_advanced.go` | `RegisterCostAdvancedRoutes` — 3 GET endpoints |
| Create | `admin/api/cost_advanced_test.go` | HTTP handler tests |
| Modify | `admin/main.go` | Wire `BudgetForecastService`, `CostBenchmarkService`, register routes |
| Modify | `dashboard/src/pages/admin/CostCenterPage.tsx` | Add Budget Forecast panel + Benchmark card |

---

### Task 1: Budget Forecast Service

**Files:**
- Create: `admin/services/budget_forecast.go`
- Create: `admin/services/budget_forecast_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/services/budget_forecast_test.go
package services_test

import (
	"math"
	"testing"

	"github.com/yourorg/totra/admin/services"
	"github.com/stretchr/testify/assert"
)

func TestLinearRegression_FlatLine(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{10, 10, 10, 10})
	assert.InDelta(t, 0.0, slope, 0.001)
	assert.InDelta(t, 10.0, intercept, 0.001)
}

func TestLinearRegression_Increasing(t *testing.T) {
	// y = 2x + 1 at x=0,1,2,3 → slope≈2, intercept≈1
	slope, intercept := services.LinearRegression([]float64{1, 3, 5, 7})
	assert.InDelta(t, 2.0, slope, 0.001)
	assert.InDelta(t, 1.0, intercept, 0.001)
}

func TestLinearRegression_SinglePoint(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{42})
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 42.0, intercept)
}

func TestLinearRegression_Empty(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{})
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 0.0, intercept)
}

func TestLinearRegression_NaN(t *testing.T) {
	slope, _ := services.LinearRegression([]float64{1, 3, 5, 7})
	assert.False(t, math.IsNaN(slope))
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestLinearRegression -v
```

Expected: compile error — `services.LinearRegression` undefined

- [ ] **Step 3: Implement the service**

```go
// admin/services/budget_forecast.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LinearRegression fits y = slope*x + intercept to y[0..n-1] using OLS.
// x values are 0-indexed integers. Returns (0,0) for empty input, (0,y[0]) for single point.
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

type BudgetForecast struct {
	TenantID      string         `json:"tenant_id"`
	History       []MonthlySpend `json:"history"`
	ForecastedEOY float64        `json:"forecasted_eoy_usd"`
	GeneratedAt   string         `json:"generated_at"`
}

type BudgetForecastService struct{ pool *pgxpool.Pool }

func NewBudgetForecastService(pool *pgxpool.Pool) *BudgetForecastService {
	return &BudgetForecastService{pool: pool}
}

func (s *BudgetForecastService) GetBudgetForecast(ctx context.Context, tenantID string) (*BudgetForecast, error) {
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

	// Project to Dec using linear regression on monthly totals
	y := make([]float64, len(history))
	for i, m := range history {
		y[i] = m.TotalUSD
	}
	slope, intercept := LinearRegression(y)

	// Months remaining in year: current month is index len(history)-1
	// We predict Dec = index 11 in the calendar year, but we use relative index
	// relative to the first historical month.
	currentMonth := time.Now().UTC().Month() // 1-12
	monthsToEOY := 12 - int(currentMonth)   // months after this one
	lastIndex := float64(len(history) - 1)

	var forecastedEOY float64
	// Sum history so far + predict remaining months
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

	return &BudgetForecast{
		TenantID:      tenantID,
		History:       history,
		ForecastedEOY: forecastedEOY,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestLinearRegression -v
```

Expected: all 5 tests PASS

- [ ] **Step 5: Commit**

```bash
git -C /Users/sugac.275/ToTra add admin/services/budget_forecast.go admin/services/budget_forecast_test.go
git -C /Users/sugac.275/ToTra commit -m "feat(cost): BudgetForecastService + LinearRegression — 6-month history + EOY projection"
```

---

### Task 2: Cost Benchmark Service

**Files:**
- Create: `admin/services/cost_benchmark.go`
- Create: `admin/services/cost_benchmark_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/services/cost_benchmark_test.go
package services_test

import (
	"testing"

	"github.com/yourorg/totra/admin/services"
	"github.com/stretchr/testify/assert"
)

func TestPercentileRank_Empty(t *testing.T) {
	assert.Equal(t, 0.0, services.PercentileRank(10, []float64{}))
}

func TestPercentileRank_AllBelow(t *testing.T) {
	// value=10 is above all → 100%
	assert.InDelta(t, 100.0, services.PercentileRank(10, []float64{1, 2, 3, 4}), 0.001)
}

func TestPercentileRank_NoneBelow(t *testing.T) {
	// value=1 is at or below all → 0%
	assert.InDelta(t, 0.0, services.PercentileRank(1, []float64{1, 2, 3, 4}), 0.001)
}

func TestPercentileRank_Middle(t *testing.T) {
	// value=3, below: {1,2} → 2/4 = 50%
	assert.InDelta(t, 50.0, services.PercentileRank(3, []float64{1, 2, 3, 4}), 0.001)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestPercentileRank -v
```

Expected: compile error — `services.PercentileRank` undefined

- [ ] **Step 3: Implement the service**

```go
// admin/services/cost_benchmark.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PercentileRank returns what percentage of all values are strictly below value.
func PercentileRank(value float64, all []float64) float64 {
	if len(all) == 0 {
		return 0
	}
	below := 0
	for _, v := range all {
		if v < value {
			below++
		}
	}
	return float64(below) / float64(len(all)) * 100
}

type CostBenchmark struct {
	YearMonth       string  `json:"year_month"`
	TenantPerUserUSD float64 `json:"tenant_per_user_usd"`
	P25             float64 `json:"p25"`
	P50             float64 `json:"p50"`
	P75             float64 `json:"p75"`
	TenantCount     int     `json:"tenant_count"`
	PercentileRank  float64 `json:"percentile_rank"`
	InsufficientData bool   `json:"insufficient_data"`
}

type ModelROI struct {
	Model              string  `json:"model"`
	TotalRequests      int64   `json:"total_requests"`
	USDPer1KRequests   float64 `json:"usd_per_1k_requests"`
}

type CostBenchmarkService struct{ pool *pgxpool.Pool }

func NewCostBenchmarkService(pool *pgxpool.Pool) *CostBenchmarkService {
	return &CostBenchmarkService{pool: pool}
}

func (s *CostBenchmarkService) GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*CostBenchmark, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}

	// Get this tenant's per-user spend for the month
	var tenantPerUser float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost) / NULLIF(COUNT(DISTINCT user_id), 0), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&tenantPerUser)
	if err != nil {
		return nil, fmt.Errorf("cost benchmark tenant query: %w", err)
	}

	// Get cross-tenant percentiles (excluding this tenant for unbiased comparison)
	var p25, p50, p75 float64
	var tenantCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(PERCENTILE_CONT(0.25) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COALESCE(PERCENTILE_CONT(0.75) WITHIN GROUP (ORDER BY per_user_usd), 0),
			COUNT(*)
		FROM (
			SELECT tenant_id, SUM(usd_cost) / NULLIF(COUNT(DISTINCT user_id), 0) AS per_user_usd
			FROM usage_records
			WHERE to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $1
			  AND tenant_id != $2
			GROUP BY tenant_id
		) sub
	`, yearMonth, tenantID).Scan(&p25, &p50, &p75, &tenantCount)
	if err != nil {
		return nil, fmt.Errorf("cost benchmark cross-tenant query: %w", err)
	}

	if tenantCount < 3 {
		return &CostBenchmark{
			YearMonth:        yearMonth,
			TenantPerUserUSD: tenantPerUser,
			InsufficientData: true,
		}, nil
	}

	return &CostBenchmark{
		YearMonth:        yearMonth,
		TenantPerUserUSD: tenantPerUser,
		P25:              p25,
		P50:              p50,
		P75:              p75,
		TenantCount:      tenantCount,
		PercentileRank:   PercentileRank(tenantPerUser, []float64{p25, p50, p75}),
		InsufficientData: false,
	}, nil
}

func (s *CostBenchmarkService) GetModelROI(ctx context.Context) ([]ModelROI, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mc.name AS model,
		       COUNT(*) AS total_requests,
		       SUM(r.usd_cost) / COUNT(*) * 1000 AS usd_per_1k_requests
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.request_at >= NOW() - INTERVAL '30 days'
		GROUP BY mc.name
		HAVING COUNT(*) >= 100
		ORDER BY usd_per_1k_requests ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("model roi query: %w", err)
	}
	defer rows.Close()

	var results []ModelROI
	for rows.Next() {
		var roi ModelROI
		if err := rows.Scan(&roi.Model, &roi.TotalRequests, &roi.USDPer1KRequests); err != nil {
			return nil, fmt.Errorf("model roi scan: %w", err)
		}
		results = append(results, roi)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("model roi rows: %w", err)
	}
	return results, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestPercentileRank -v
```

Expected: all 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git -C /Users/sugac.275/ToTra add admin/services/cost_benchmark.go admin/services/cost_benchmark_test.go
git -C /Users/sugac.275/ToTra commit -m "feat(cost): CostBenchmarkService — per-user monthly percentiles + ModelROI ranking"
```

---

### Task 3: Cost Advanced API Endpoints

**Files:**
- Create: `admin/api/cost_advanced.go`
- Create: `admin/api/cost_advanced_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/api/cost_advanced_test.go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBudgetSvc struct{}

func (s *stubBudgetSvc) GetBudgetForecast(ctx context.Context, tenantID string) (*services.BudgetForecast, error) {
	return &services.BudgetForecast{
		TenantID:      tenantID,
		ForecastedEOY: 1234.56,
		History:       []services.MonthlySpend{{Month: "2026-05", TotalUSD: 100}},
		GeneratedAt:   "2026-05-14T00:00:00Z",
	}, nil
}

type stubCostBenchmarkSvc struct{}

func (s *stubCostBenchmarkSvc) GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.CostBenchmark, error) {
	return &services.CostBenchmark{
		YearMonth:        "2026-05",
		TenantPerUserUSD: 50.0,
		P25:              20.0,
		P50:              40.0,
		P75:              80.0,
		TenantCount:      10,
		PercentileRank:   60.0,
		InsufficientData: false,
	}, nil
}

func (s *stubCostBenchmarkSvc) GetModelROI(ctx context.Context) ([]services.ModelROI, error) {
	return []services.ModelROI{
		{Model: "gpt-4o-mini", TotalRequests: 500, USDPer1KRequests: 0.15},
	}, nil
}

func makeCostAdvancedApp() *fiber.App {
	app := fiber.New()
	// Inject claims so auth middleware is satisfied
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostAdvancedRoutes(app, &stubBudgetSvc{}, &stubCostBenchmarkSvc{})
	return app
}

func TestCostBudgetForecast(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-forecast", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.BudgetForecast
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	assert.InDelta(t, 1234.56, body.ForecastedEOY, 0.01)
}

func TestCostBenchmark(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/benchmark", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.CostBenchmark
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "2026-05", body.YearMonth)
	assert.False(t, body.InsufficientData)
}

func TestCostModelROI(t *testing.T) {
	app := makeCostAdvancedApp()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/model-roi", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body []services.ModelROI
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body, 1)
	assert.Equal(t, "gpt-4o-mini", body[0].Model)
}

func TestCostBudgetForecast_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "user"})
		return c.Next()
	})
	api.RegisterCostAdvancedRoutes(app, &stubBudgetSvc{}, &stubCostBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-forecast", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run TestCost -v 2>&1 | head -20
```

Expected: compile error — `api.RegisterCostAdvancedRoutes` undefined

- [ ] **Step 3: Implement the handler**

```go
// admin/api/cost_advanced.go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type BudgetForecastServiceIface interface {
	GetBudgetForecast(ctx context.Context, tenantID string) (*services.BudgetForecast, error)
}

type CostBenchmarkServiceIface interface {
	GetCostBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.CostBenchmark, error)
	GetModelROI(ctx context.Context) ([]services.ModelROI, error)
}

func RegisterCostAdvancedRoutes(r fiber.Router, budgetSvc BudgetForecastServiceIface, benchmarkSvc CostBenchmarkServiceIface) {
	r.Get("/api/admin/cost/budget-forecast", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := budgetSvc.GetBudgetForecast(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/benchmark", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := benchmarkSvc.GetCostBenchmark(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/model-roi", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims == nil || claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := benchmarkSvc.GetModelROI(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})
}
```

- [ ] **Step 4: Wire into `admin/main.go`**

Find the block that wires `costSavingsSvc` (search for `RegisterCostSavingsRoutes`) and add after it:

```go
budgetForecastSvc := services.NewBudgetForecastService(pool)
costBenchmarkSvc := services.NewCostBenchmarkService(pool)
api.RegisterCostAdvancedRoutes(protected, budgetForecastSvc, costBenchmarkSvc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run TestCost -v
```

Expected: all 4 tests PASS

- [ ] **Step 6: Verify build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
git -C /Users/sugac.275/ToTra add admin/api/cost_advanced.go admin/api/cost_advanced_test.go admin/main.go
git -C /Users/sugac.275/ToTra commit -m "feat(cost): budget-forecast + benchmark + model-roi endpoints"
```

---

### Task 4: Dashboard — Budget Forecast Panel + Benchmark Card

**Files:**
- Modify: `dashboard/src/pages/admin/CostCenterPage.tsx`

- [ ] **Step 1: Write the failing test (snapshot)**

There are no existing snapshot tests in this project. We verify by running the TypeScript build.

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1 | head -20
```

Expected: 0 errors (baseline)

- [ ] **Step 2: Read CostCenterPage.tsx to understand current state**

Read the file at `dashboard/src/pages/admin/CostCenterPage.tsx` fully before making edits.

- [ ] **Step 3: Add Budget Forecast + Benchmark sections**

Extend `CostCenterPage.tsx` with two new `useQuery` calls and panels. Add at the top of the file with other imports:

```tsx
// Add to existing imports
```

Add query hooks inside the component (alongside existing `useQuery` calls for savings):

```tsx
const { data: forecast, isLoading: forecastLoading } = useQuery({
  queryKey: ["cost-budget-forecast"],
  queryFn: () =>
    fetch("/api/admin/cost/budget-forecast", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});

const { data: benchmark, isLoading: benchmarkLoading } = useQuery({
  queryKey: ["cost-benchmark"],
  queryFn: () =>
    fetch("/api/admin/cost/benchmark", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});

const { data: modelROI, isLoading: roiLoading } = useQuery({
  queryKey: ["cost-model-roi"],
  queryFn: () =>
    fetch("/api/admin/cost/model-roi", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});
```

Add Budget Forecast panel JSX (insert after the existing savings panel):

```tsx
{/* Budget Forecast */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-4">Budget Forecast (EOY)</h2>
  {forecastLoading ? (
    <p className="text-gray-400">Loading…</p>
  ) : forecast ? (
    <div>
      <p className="text-3xl font-bold text-blue-600">
        ${forecast.forecasted_eoy_usd?.toFixed(2) ?? "—"}
      </p>
      <p className="text-sm text-gray-500 mt-1">Projected full-year spend</p>
      <div className="mt-4 overflow-x-auto">
        <table className="w-full text-sm text-left">
          <thead>
            <tr className="text-gray-500 border-b">
              <th className="pb-2 pr-4">Month</th>
              <th className="pb-2">USD</th>
            </tr>
          </thead>
          <tbody>
            {forecast.history?.map((m: { month: string; total_usd: number }) => (
              <tr key={m.month} className="border-b last:border-0">
                <td className="py-1 pr-4">{m.month}</td>
                <td className="py-1">${m.total_usd.toFixed(4)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  ) : (
    <p className="text-gray-400">No data</p>
  )}
</div>

{/* Cost Benchmark */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-4">Cost Benchmark (This Month)</h2>
  {benchmarkLoading ? (
    <p className="text-gray-400">Loading…</p>
  ) : benchmark?.insufficient_data ? (
    <p className="text-gray-400 text-sm">Insufficient tenant data for comparison.</p>
  ) : benchmark ? (
    <div className="space-y-3">
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">Your per-user spend</span>
        <span className="font-medium">${benchmark.tenant_per_user_usd?.toFixed(4)}</span>
      </div>
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">Market P25 / P50 / P75</span>
        <span className="font-medium">
          ${benchmark.p25?.toFixed(4)} / ${benchmark.p50?.toFixed(4)} / ${benchmark.p75?.toFixed(4)}
        </span>
      </div>
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">Your percentile rank</span>
        <span className={`font-medium ${benchmark.percentile_rank > 75 ? "text-red-600" : "text-green-600"}`}>
          {benchmark.percentile_rank?.toFixed(1)}%
        </span>
      </div>
      <div className="flex justify-between text-sm text-gray-400">
        <span>Based on {benchmark.tenant_count} tenants</span>
      </div>
    </div>
  ) : (
    <p className="text-gray-400">No data</p>
  )}
</div>

{/* Model ROI */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-4">Model ROI (Last 30 Days)</h2>
  {roiLoading ? (
    <p className="text-gray-400">Loading…</p>
  ) : modelROI && modelROI.length > 0 ? (
    <div className="overflow-x-auto">
      <table className="w-full text-sm text-left">
        <thead>
          <tr className="text-gray-500 border-b">
            <th className="pb-2 pr-4">Model</th>
            <th className="pb-2 pr-4">Requests</th>
            <th className="pb-2">USD / 1K req</th>
          </tr>
        </thead>
        <tbody>
          {modelROI.map((roi: { model: string; total_requests: number; usd_per_1k_requests: number }) => (
            <tr key={roi.model} className="border-b last:border-0">
              <td className="py-1 pr-4 font-mono text-xs">{roi.model}</td>
              <td className="py-1 pr-4">{roi.total_requests.toLocaleString()}</td>
              <td className="py-1">${roi.usd_per_1k_requests.toFixed(4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <p className="text-gray-400 text-sm">No models with ≥100 requests in last 30 days.</p>
  )}
</div>
```

- [ ] **Step 4: Verify TypeScript compiles**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1 | head -30
```

Expected: 0 errors

- [ ] **Step 5: Commit**

```bash
git -C /Users/sugac.275/ToTra add dashboard/src/pages/admin/CostCenterPage.tsx
git -C /Users/sugac.275/ToTra commit -m "feat(cost): budget forecast + benchmark + model ROI panels on CostCenterPage"
```

---

## Self-Review

**Spec coverage:**
- ✅ Budget forecast (linear regression, 6-month history, EOY projection) — Task 1
- ✅ Cross-tenant cost benchmark (per-user monthly USD percentiles, p25/p50/p75) — Task 2
- ✅ Model ROI ranking (cost per 1000 requests, cross-tenant, last 30 days, min 100 req) — Task 2
- ✅ Three GET endpoints wired into admin — Task 3
- ✅ Dashboard panels for forecast + benchmark + ROI — Task 4
- ✅ `InsufficientData` guard (< 3 tenants) — Task 2 `GetCostBenchmark`
- ✅ Admin-only route guard on all three endpoints — Task 3

**Placeholder scan:** No TBD, no "similar to", no incomplete steps.

**Type consistency:**
- `services.BudgetForecast` defined in Task 1, used in Task 3 interface and Task 4 JSX — consistent.
- `services.CostBenchmark` defined in Task 2, used in Task 3 interface and Task 4 JSX — consistent.
- `services.ModelROI` defined in Task 2, used in Task 3 interface and Task 4 JSX — consistent.
- `BudgetForecastServiceIface.GetBudgetForecast(ctx, tenantID)` — matches Task 1 method signature.
- `CostBenchmarkServiceIface.GetCostBenchmark(ctx, tenantID, yearMonth)` — matches Task 2 method signature.
- `CostBenchmarkServiceIface.GetModelROI(ctx)` — matches Task 2 method signature.
