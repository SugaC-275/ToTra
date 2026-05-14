# Executive ROI Reports (Plan G) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three executive-facing analytics: quarterly AI ROI reports, industry benchmark percentile comparison, and monthly department efficiency challenge rankings.

**Architecture:** New `ROIService` queries `usage_records` + `efficiency_snapshots` for quarterly aggregation, reads a seeded `industry_benchmarks` table for percentile comparison, and queries `efficiency_snapshots JOIN users` for dept challenge. Three new admin-only API endpoints feed a single `ROIReportPage` with tabbed sections.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, React 19 + TypeScript, TanStack Query v5, Tailwind v4.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `infra/postgres/009_roi_reports.sql` (create) |
| 1 | `admin/services/roi.go` (create), `admin/services/roi_test.go` (create) |
| 2 | `admin/api/roi.go` (create), `admin/api/roi_test.go` (create), `admin/main.go` (modify) |
| 3 | `dashboard/src/api/client.ts` (modify), `dashboard/src/pages/admin/ROIReportPage.tsx` (create), `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 0: DB migration — industry_benchmarks table

**Files:**
- Create: `infra/postgres/009_roi_reports.sql`

- [ ] **Step 1: Create the migration file**

```sql
BEGIN;

CREATE TABLE IF NOT EXISTS industry_benchmarks (
    id         SERIAL PRIMARY KEY,
    label      TEXT NOT NULL DEFAULT 'software' UNIQUE,
    p25        FLOAT NOT NULL,
    p50        FLOAT NOT NULL,
    p75        FLOAT NOT NULL,
    p90        FLOAT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed realistic percentile values for efficiency_score
-- Based on: score = total_output_weight / log(total_scu + 1)
-- p25=0.05 means bottom-quartile companies, p90=0.45 means top-decile
INSERT INTO industry_benchmarks (label, p25, p50, p75, p90)
VALUES ('software', 0.05, 0.12, 0.25, 0.45)
ON CONFLICT (label) DO NOTHING;

COMMIT;
```

- [ ] **Step 2: Apply migration**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra < /Users/sugac.275/ToTra/infra/postgres/009_roi_reports.sql
```

Expected: `BEGIN`, `CREATE TABLE`, `INSERT 0 1`, `COMMIT`.

- [ ] **Step 3: Verify**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra -c "SELECT * FROM industry_benchmarks;"
```

Expected: one row with p25=0.05, p50=0.12, p75=0.25, p90=0.45.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add infra/postgres/009_roi_reports.sql
git commit -m "feat(db): add industry_benchmarks table for ROI benchmark comparison"
```

---

## Task 1: Service layer — ROIService

**Files:**
- Create: `admin/services/roi.go`
- Create: `admin/services/roi_test.go`

Before writing, read `admin/services/usage.go` to match the struct/method patterns.

- [ ] **Step 1: Write `admin/services/roi_test.go`**

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

func TestComputeROI(t *testing.T) {
	assert.InDelta(t, 2.5, services.ComputeROI(5.0, 2.0), 0.001)
}

func TestComputeROI_ZeroUSD(t *testing.T) {
	assert.Equal(t, 0.0, services.ComputeROI(5.0, 0.0))
}

func TestComputePercentile_Top10(t *testing.T) {
	// score above p90 → ≥ 90th percentile
	pct := services.ComputePercentile(0.50, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 90, pct)
}

func TestComputePercentile_Between50And75(t *testing.T) {
	pct := services.ComputePercentile(0.20, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 50, pct)
}

func TestComputePercentile_Bottom(t *testing.T) {
	pct := services.ComputePercentile(0.02, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 10, pct)
}

func TestBenchmarkLabel(t *testing.T) {
	assert.Equal(t, "Top 10%", services.BenchmarkLabel(90))
	assert.Equal(t, "Top 25%", services.BenchmarkLabel(75))
	assert.Equal(t, "Top 50%", services.BenchmarkLabel(50))
	assert.Equal(t, "Bottom 50%", services.BenchmarkLabel(25))
	assert.Equal(t, "Bottom 25%", services.BenchmarkLabel(10))
}
```

- [ ] **Step 2: Run tests to confirm compilation fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestCompute|TestBenchmark" -v 2>&1 | head -10
```

Expected: compilation error (ComputeROI not defined).

- [ ] **Step 3: Create `admin/services/roi.go`**

```go
package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Structs ---

type QuarterROI struct {
	Quarter     string  `json:"quarter"`      // e.g. "2026-Q2"
	TotalUSD    float64 `json:"total_usd"`
	TotalOutput float64 `json:"total_output"` // sum of output_weight from snapshots
	ROIScore    float64 `json:"roi_score"`    // TotalOutput / TotalUSD
	ActiveUsers int     `json:"active_users"`
}

type BenchmarkResult struct {
	TenantAvgEfficiency float64 `json:"tenant_avg_efficiency"`
	Percentile          int     `json:"percentile"`
	IndustryP25         float64 `json:"industry_p25"`
	IndustryP50         float64 `json:"industry_p50"`
	IndustryP75         float64 `json:"industry_p75"`
	IndustryP90         float64 `json:"industry_p90"`
	Label               string  `json:"label"` // e.g. "Top 27%"
}

type DeptChallengeEntry struct {
	Department  string  `json:"department"`
	AvgAIQScore float64 `json:"avg_aiq_score"`
	UserCount   int     `json:"user_count"`
	Rank        int     `json:"rank"`
	IsWinner    bool    `json:"is_winner"`
}

// --- Pure functions (exported for testing) ---

// ComputeROI returns output/usd; 0 if usd is zero.
func ComputeROI(output, usd float64) float64 {
	if usd == 0 {
		return 0
	}
	return output / usd
}

// ComputePercentile returns the industry percentile bucket (10, 25, 50, 75, 90).
func ComputePercentile(score, p25, p50, p75, p90 float64) int {
	switch {
	case score >= p90:
		return 90
	case score >= p75:
		return 75
	case score >= p50:
		return 50
	case score >= p25:
		return 25
	default:
		return 10
	}
}

// BenchmarkLabel converts a percentile bucket to a human-readable label.
func BenchmarkLabel(pct int) string {
	switch pct {
	case 90:
		return "Top 10%"
	case 75:
		return "Top 25%"
	case 50:
		return "Top 50%"
	case 25:
		return "Bottom 50%"
	default:
		return "Bottom 25%"
	}
}

// --- Service ---

type ROIService struct {
	pool *pgxpool.Pool
}

func NewROIService(pool *pgxpool.Pool) *ROIService {
	return &ROIService{pool: pool}
}

// GetQuarterlyROI returns per-quarter ROI for the given tenant and year (e.g. "2026").
// It aggregates usd_cost from usage_records and output_weight from efficiency_snapshots.
func (s *ROIService) GetQuarterlyROI(ctx context.Context, tenantID, year string) ([]*QuarterROI, error) {
	// Step 1: get cost per quarter from usage_records
	costRows, err := s.pool.Query(ctx, `
		SELECT
			EXTRACT(QUARTER FROM request_at)::int AS q,
			COALESCE(SUM(usd_cost), 0)            AS total_usd,
			COUNT(DISTINCT user_id)               AS active_users
		FROM usage_records
		WHERE tenant_id = $1
		  AND EXTRACT(YEAR FROM request_at)::int = $2::int
		GROUP BY q
		ORDER BY q`,
		tenantID, year,
	)
	if err != nil {
		return nil, err
	}
	defer costRows.Close()

	type costRow struct {
		q           int
		totalUSD    float64
		activeUsers int
	}
	costMap := map[int]*costRow{}
	for costRows.Next() {
		var r costRow
		if err := costRows.Scan(&r.q, &r.totalUSD, &r.activeUsers); err != nil {
			return nil, err
		}
		costMap[r.q] = &r
	}
	if err := costRows.Err(); err != nil {
		return nil, err
	}

	// Step 2: get output weight per quarter from efficiency_snapshots
	outRows, err := s.pool.Query(ctx, `
		SELECT
			EXTRACT(QUARTER FROM to_date(year_month, 'YYYY-MM'))::int AS q,
			COALESCE(SUM(total_output_weight), 0)                     AS total_output
		FROM efficiency_snapshots
		WHERE tenant_id = $1
		  AND year_month LIKE $2 || '-%'
		  AND anomaly_flagged = false
		GROUP BY q
		ORDER BY q`,
		tenantID, year,
	)
	if err != nil {
		return nil, err
	}
	defer outRows.Close()

	outputMap := map[int]float64{}
	for outRows.Next() {
		var q int
		var out float64
		if err := outRows.Scan(&q, &out); err != nil {
			return nil, err
		}
		outputMap[q] = out
	}
	if err := outRows.Err(); err != nil {
		return nil, err
	}

	// Step 3: merge into QuarterROI for all quarters that have any data
	seen := map[int]bool{}
	for q := range costMap {
		seen[q] = true
	}
	for q := range outputMap {
		seen[q] = true
	}

	var result []*QuarterROI
	for q := 1; q <= 4; q++ {
		if !seen[q] {
			continue
		}
		var totalUSD float64
		var activeUsers int
		if cr, ok := costMap[q]; ok {
			totalUSD = cr.totalUSD
			activeUsers = cr.activeUsers
		}
		totalOutput := outputMap[q]
		result = append(result, &QuarterROI{
			Quarter:     year + "-Q" + string(rune('0'+q)),
			TotalUSD:    totalUSD,
			TotalOutput: totalOutput,
			ROIScore:    ComputeROI(totalOutput, totalUSD),
			ActiveUsers: activeUsers,
		})
	}
	return result, nil
}

// GetBenchmark compares the tenant's avg efficiency score for a month against industry percentiles.
func (s *ROIService) GetBenchmark(ctx context.Context, tenantID, yearMonth string) (*BenchmarkResult, error) {
	// Get tenant's average efficiency score for the month
	var tenantAvg float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(efficiency_score), 0)
		FROM efficiency_snapshots
		WHERE tenant_id = $1 AND year_month = $2 AND anomaly_flagged = false`,
		tenantID, yearMonth,
	).Scan(&tenantAvg)
	if err != nil {
		return nil, err
	}

	// Get industry benchmark percentiles (use most recent row)
	var p25, p50, p75, p90 float64
	err = s.pool.QueryRow(ctx,
		`SELECT p25, p50, p75, p90 FROM industry_benchmarks ORDER BY id DESC LIMIT 1`,
	).Scan(&p25, &p50, &p75, &p90)
	if err != nil {
		return nil, err
	}

	pct := ComputePercentile(tenantAvg, p25, p50, p75, p90)
	return &BenchmarkResult{
		TenantAvgEfficiency: tenantAvg,
		Percentile:          pct,
		IndustryP25:         p25,
		IndustryP50:         p50,
		IndustryP75:         p75,
		IndustryP90:         p90,
		Label:               BenchmarkLabel(pct),
	}, nil
}

// GetDeptChallenge ranks departments by average AIQ score for the given month.
func (s *ROIService) GetDeptChallenge(ctx context.Context, tenantID, yearMonth string) ([]*DeptChallengeEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(u.department, '(未设置)') AS department,
			COALESCE(AVG(s.aiq_score), 0)     AS avg_aiq_score,
			COUNT(DISTINCT s.user_id)          AS user_count
		FROM efficiency_snapshots s
		JOIN users u ON u.id = s.user_id
		WHERE s.tenant_id = $1
		  AND s.year_month = $2
		  AND s.anomaly_flagged = false
		GROUP BY COALESCE(u.department, '(未设置)')
		ORDER BY avg_aiq_score DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DeptChallengeEntry
	for rows.Next() {
		e := &DeptChallengeEntry{}
		if err := rows.Scan(&e.Department, &e.AvgAIQScore, &e.UserCount); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Assign ranks and mark winner (rank 1 = winner)
	for i, e := range result {
		e.Rank = i + 1
		e.IsWinner = (i == 0)
	}
	return result, nil
}
```

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestCompute|TestBenchmark" -v
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Run all service tests + build**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -v 2>&1 | tail -15
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 6: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/services/roi.go admin/services/roi_test.go
git commit -m "feat(services): add ROIService — quarterly ROI, benchmark, dept challenge"
```

---

## Task 2: API routes + main.go

**Files:**
- Create: `admin/api/roi.go`
- Create: `admin/api/roi_test.go`
- Modify: `admin/main.go`

Read `admin/api/usage.go` to match the admin-only handler pattern.

- [ ] **Step 1: Write `admin/api/roi_test.go`**

```go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubROISvc struct{}

func (s *stubROISvc) GetQuarterlyROI(_ context.Context, _, _ string) ([]*services.QuarterROI, error) {
	return []*services.QuarterROI{{Quarter: "2026-Q2", TotalUSD: 10, TotalOutput: 5, ROIScore: 0.5, ActiveUsers: 3}}, nil
}
func (s *stubROISvc) GetBenchmark(_ context.Context, _, _ string) (*services.BenchmarkResult, error) {
	return &services.BenchmarkResult{TenantAvgEfficiency: 0.20, Percentile: 50, Label: "Top 50%"}, nil
}
func (s *stubROISvc) GetDeptChallenge(_ context.Context, _, _ string) ([]*services.DeptChallengeEntry, error) {
	return []*services.DeptChallengeEntry{{Department: "Engineering", AvgAIQScore: 80, UserCount: 2, Rank: 1, IsWinner: true}}, nil
}

func setupROIApp(svc *stubROISvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterROIRoutes(app, svc)
	return app
}

func TestROIQuarterly_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/quarterly?year=2026", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Len(t, result, 1)
	assert.Equal(t, "2026-Q2", result[0]["quarter"])
}

func TestROIBenchmark_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/benchmark?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "Top 50%", result["label"])
}

func TestROIChallenge_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/challenge?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Len(t, result, 1)
	assert.Equal(t, true, result[0]["is_winner"])
}

func TestROI_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterROIRoutes(app, &stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/quarterly?year=2026", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run tests to confirm compilation fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestROI" -v 2>&1 | head -10
```

- [ ] **Step 3: Create `admin/api/roi.go`**

```go
package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ROIServiceIface interface {
	GetQuarterlyROI(ctx context.Context, tenantID, year string) ([]*services.QuarterROI, error)
	GetBenchmark(ctx context.Context, tenantID, yearMonth string) (*services.BenchmarkResult, error)
	GetDeptChallenge(ctx context.Context, tenantID, yearMonth string) ([]*services.DeptChallengeEntry, error)
}

func RegisterROIRoutes(app fiber.Router, svc ROIServiceIface) {
	app.Get("/api/admin/roi/quarterly", getQuarterlyROI(svc))
	app.Get("/api/admin/roi/benchmark", getROIBenchmark(svc))
	app.Get("/api/admin/roi/challenge", getDeptChallenge(svc))
}

func getQuarterlyROI(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		year := c.Query("year", time.Now().UTC().Format("2006"))
		result, err := svc.GetQuarterlyROI(c.Context(), claims.TenantID, year)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if result == nil {
			result = []*services.QuarterROI{}
		}
		return c.JSON(result)
	}
}

func getROIBenchmark(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		result, err := svc.GetBenchmark(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}

func getDeptChallenge(svc ROIServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		result, err := svc.GetDeptChallenge(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if result == nil {
			result = []*services.DeptChallengeEntry{}
		}
		return c.JSON(result)
	}
}
```

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestROI" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Update `admin/main.go`**

Read `admin/main.go` first. Find where `hrSyncSvc` is initialized. Add after it:

```go
roiSvc := services.NewROIService(pool)
```

Find where `api.RegisterHRSyncRoutes(protected, hrSyncSvc)` is called. Add after it:

```go
api.RegisterROIRoutes(protected, roiSvc)
```

- [ ] **Step 6: Run all API tests + build**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -v 2>&1 | tail -15
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/api/roi.go admin/api/roi_test.go admin/main.go
git commit -m "feat(api): add ROI report endpoints — quarterly, benchmark, dept challenge"
```

---

## Task 3: Frontend — ROIReportPage

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/ROIReportPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

Read `dashboard/src/pages/admin/DepartmentReportPage.tsx` for style reference.

- [ ] **Step 1: Add ROI types and API functions to `dashboard/src/api/client.ts`**

After the last export (syncHRCSV), add:

```typescript
export interface QuarterROI {
  quarter: string;
  total_usd: number;
  total_output: number;
  roi_score: number;
  active_users: number;
}

export interface BenchmarkResult {
  tenant_avg_efficiency: number;
  percentile: number;
  industry_p25: number;
  industry_p50: number;
  industry_p75: number;
  industry_p90: number;
  label: string;
}

export interface DeptChallengeEntry {
  department: string;
  avg_aiq_score: number;
  user_count: number;
  rank: number;
  is_winner: boolean;
}

export const getQuarterlyROI = async (year: string): Promise<QuarterROI[]> => {
  const { data } = await apiClient.get(`/api/admin/roi/quarterly?year=${year}`);
  return data;
};

export const getROIBenchmark = async (month: string): Promise<BenchmarkResult> => {
  const { data } = await apiClient.get(`/api/admin/roi/benchmark?month=${month}`);
  return data;
};

export const getDeptChallenge = async (month: string): Promise<DeptChallengeEntry[]> => {
  const { data } = await apiClient.get(`/api/admin/roi/challenge?month=${month}`);
  return data;
};
```

- [ ] **Step 2: Create `dashboard/src/pages/admin/ROIReportPage.tsx`**

```tsx
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getQuarterlyROI,
  getROIBenchmark,
  getDeptChallenge,
} from "../../api/client";

const currentYear = new Date().getFullYear().toString();
const currentMonth = new Date().toISOString().slice(0, 7);

export default function ROIReportPage() {
  const [year, setYear] = useState(currentYear);
  const [month, setMonth] = useState(currentMonth);

  const { data: quarterly = [] } = useQuery({
    queryKey: ["roi-quarterly", year],
    queryFn: () => getQuarterlyROI(year),
  });

  const { data: benchmark } = useQuery({
    queryKey: ["roi-benchmark", month],
    queryFn: () => getROIBenchmark(month),
  });

  const { data: challenge = [] } = useQuery({
    queryKey: ["roi-challenge", month],
    queryFn: () => getDeptChallenge(month),
  });

  return (
    <div className="p-6 space-y-8">
      <h1 className="text-2xl font-bold">Executive ROI Reports</h1>

      {/* Quarterly ROI */}
      <section className="bg-white rounded-lg border p-4 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-lg">Quarterly ROI</h2>
          <input
            type="number"
            value={year}
            min="2020"
            max="2030"
            onChange={(e) => setYear(e.target.value)}
            className="border rounded px-2 py-1 w-24 text-sm"
          />
        </div>
        {quarterly.length === 0 ? (
          <p className="text-gray-400 text-sm">No data for {year}.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-gray-500 border-b">
                  <th className="pb-2 pr-4">Quarter</th>
                  <th className="pb-2 pr-4">AI Cost (USD)</th>
                  <th className="pb-2 pr-4">Output Weight</th>
                  <th className="pb-2 pr-4">ROI Score</th>
                  <th className="pb-2">Active Users</th>
                </tr>
              </thead>
              <tbody>
                {quarterly.map((q) => (
                  <tr key={q.quarter} className="border-b last:border-0">
                    <td className="py-2 pr-4 font-medium">{q.quarter}</td>
                    <td className="py-2 pr-4">${q.total_usd.toFixed(2)}</td>
                    <td className="py-2 pr-4">{q.total_output.toFixed(2)}</td>
                    <td className="py-2 pr-4 font-semibold text-blue-700">
                      {q.roi_score.toFixed(3)}
                    </td>
                    <td className="py-2">{q.active_users}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Month selector shared by benchmark + challenge */}
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-500">Month:</span>
        <input
          type="month"
          value={month}
          onChange={(e) => setMonth(e.target.value)}
          className="border rounded px-2 py-1 text-sm"
        />
      </div>

      {/* Industry Benchmark */}
      <section className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Industry Benchmark</h2>
        {benchmark ? (
          <div className="space-y-3">
            <div className="flex items-center gap-4">
              <div className="text-center">
                <p className="text-3xl font-bold text-blue-700">
                  {benchmark.label}
                </p>
                <p className="text-gray-500 text-sm">Your standing</p>
              </div>
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-800">
                  {benchmark.tenant_avg_efficiency.toFixed(3)}
                </p>
                <p className="text-gray-500 text-sm">Your avg efficiency</p>
              </div>
            </div>
            <div className="grid grid-cols-4 gap-2 text-center text-xs text-gray-500">
              {[
                { label: "P25", value: benchmark.industry_p25 },
                { label: "P50", value: benchmark.industry_p50 },
                { label: "P75", value: benchmark.industry_p75 },
                { label: "P90", value: benchmark.industry_p90 },
              ].map(({ label, value }) => (
                <div key={label} className="bg-gray-50 rounded p-2">
                  <p className="font-medium text-gray-700">{value.toFixed(3)}</p>
                  <p>{label}</p>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <p className="text-gray-400 text-sm">Loading...</p>
        )}
      </section>

      {/* Department Efficiency Challenge */}
      <section className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Department Efficiency Challenge</h2>
        {challenge.length === 0 ? (
          <p className="text-gray-400 text-sm">No department data for {month}.</p>
        ) : (
          <div className="space-y-2">
            {challenge.map((d) => (
              <div
                key={d.department}
                className={`flex items-center justify-between rounded-lg p-3 ${
                  d.is_winner
                    ? "bg-yellow-50 border border-yellow-300"
                    : "bg-gray-50"
                }`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-lg font-bold text-gray-400">
                    #{d.rank}
                  </span>
                  <div>
                    <p className="font-medium">
                      {d.department}
                      {d.is_winner && (
                        <span className="ml-2 text-xs bg-yellow-200 text-yellow-800 rounded px-1 py-0.5">
                          Winner
                        </span>
                      )}
                    </p>
                    <p className="text-xs text-gray-500">{d.user_count} users</p>
                  </div>
                </div>
                <p className="text-xl font-bold text-blue-700">
                  {d.avg_aiq_score.toFixed(1)}
                </p>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
```

- [ ] **Step 3: Update `dashboard/src/App.tsx`**

Add import:
```tsx
import ROIReportPage from "./pages/admin/ROIReportPage";
```

Add route after `admin/hr-sync`:
```tsx
<Route path="admin/roi" element={<ProtectedRoute adminOnly><ROIReportPage /></ProtectedRoute>} />
```

- [ ] **Step 4: Update `dashboard/src/components/Layout.tsx`**

Add "ROI Reports" nav link after "HR Sync", pointing to `/admin/roi`, using the same pattern as surrounding links.

- [ ] **Step 5: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1
```

Fix any errors before proceeding.

- [ ] **Step 6: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/api/client.ts dashboard/src/pages/admin/ROIReportPage.tsx dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "feat(dashboard): add ROIReportPage — quarterly ROI, benchmark, dept challenge"
```
