# Compliance Platform Phase 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Risk score trend (6-month monthly time series) + Compliance digest report (PII breakdown by type, top violators, summary stats). Both computed on-the-fly from existing `pii_violations` and `usage_records` tables — no new migration.

**Architecture:** One new service file with two services and testable pure functions. One new API file with two GET endpoints. `CompliancePage.tsx` extended with a risk trend sparkline and a digest card.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5 · React 19 + TanStack Query v5 + Tailwind v4

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `admin/services/compliance_trend.go` | `MonthlyRiskScore` pure fn · `RiskTrendService.GetRiskTrend` · `ComplianceDigestService.GetDigest` |
| Create | `admin/services/compliance_trend_test.go` | Unit tests for `MonthlyRiskScore` |
| Create | `admin/api/compliance_reports.go` | `RegisterComplianceReportRoutes` — GET /risk-trend, GET /digest |
| Create | `admin/api/compliance_reports_test.go` | Handler tests |
| Modify | `admin/main.go` | Wire both services + routes |
| Modify | `dashboard/src/pages/admin/CompliancePage.tsx` | Risk trend sparkline + digest card |

---

### Task 1: Compliance Trend + Digest Service (TDD)

**Files:**
- Create: `admin/services/compliance_trend.go`
- Create: `admin/services/compliance_trend_test.go`

- [ ] **Step 1: Write failing unit tests**

```go
// admin/services/compliance_trend_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestMonthlyRiskScore_Zero(t *testing.T) {
	assert.Equal(t, 0.0, services.MonthlyRiskScore(0))
}

func TestMonthlyRiskScore_Low(t *testing.T) {
	// 5 violations per 1k requests → score 10
	assert.InDelta(t, 10.0, services.MonthlyRiskScore(5), 0.001)
}

func TestMonthlyRiskScore_Saturates(t *testing.T) {
	// 100 violations per 1k → capped at 100
	assert.Equal(t, 100.0, services.MonthlyRiskScore(100))
	assert.Equal(t, 100.0, services.MonthlyRiskScore(999))
}

func TestMonthlyRiskScore_Midpoint(t *testing.T) {
	// 25 per 1k → score 50
	assert.InDelta(t, 50.0, services.MonthlyRiskScore(25), 0.001)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./services/ -run TestMonthlyRiskScore -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 3: Implement service**

```go
// admin/services/compliance_trend.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MonthlyRiskScore converts violations-per-1000-requests to a 0–100 risk score.
// Rate of 50/1k = score 100 (saturates).
func MonthlyRiskScore(violationsPer1K float64) float64 {
	if violationsPer1K <= 0 {
		return 0
	}
	score := violationsPer1K * 2
	if score > 100 {
		return 100
	}
	return score
}

type MonthlyRiskPoint struct {
	Month      string  `json:"month"`
	Violations int64   `json:"violations"`
	Requests   int64   `json:"requests"`
	RatePer1K  float64 `json:"rate_per_1k"`
	RiskScore  float64 `json:"risk_score"`
}

type RiskTrend struct {
	TenantID    string             `json:"tenant_id"`
	Points      []MonthlyRiskPoint `json:"points"`
	GeneratedAt string             `json:"generated_at"`
}

type RiskTrendService struct{ pool *pgxpool.Pool }

func NewRiskTrendService(pool *pgxpool.Pool) *RiskTrendService {
	return &RiskTrendService{pool: pool}
}

func (s *RiskTrendService) GetRiskTrend(ctx context.Context, tenantID string) (*RiskTrend, error) {
	rows, err := s.pool.Query(ctx, `
		WITH months AS (
			SELECT to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') AS month,
			       COUNT(*) AS violations
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '6 months'
			GROUP BY month
		),
		reqs AS (
			SELECT to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') AS month,
			       COUNT(*) AS requests
			FROM usage_records
			WHERE tenant_id = $1
			  AND request_at >= NOW() - INTERVAL '6 months'
			GROUP BY month
		)
		SELECT COALESCE(m.month, r.month),
		       COALESCE(m.violations, 0),
		       COALESCE(r.requests, 0)
		FROM months m
		FULL OUTER JOIN reqs r ON r.month = m.month
		ORDER BY 1 ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("risk trend query: %w", err)
	}
	defer rows.Close()

	var points []MonthlyRiskPoint
	for rows.Next() {
		var p MonthlyRiskPoint
		if err := rows.Scan(&p.Month, &p.Violations, &p.Requests); err != nil {
			return nil, fmt.Errorf("risk trend scan: %w", err)
		}
		if p.Requests > 0 {
			p.RatePer1K = float64(p.Violations) / float64(p.Requests) * 1000
		}
		p.RiskScore = MonthlyRiskScore(p.RatePer1K)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("risk trend rows: %w", err)
	}
	if points == nil {
		points = []MonthlyRiskPoint{}
	}
	return &RiskTrend{
		TenantID:    tenantID,
		Points:      points,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Compliance Digest ---

type PIITypeStat struct {
	PIIType string `json:"pii_type"`
	Count   int64  `json:"count"`
}

type UserViolationStat struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	UserEmail  string `json:"user_email"`
	Department string `json:"department"`
	Count      int64  `json:"count"`
}

type ComplianceDigest struct {
	TenantID        string              `json:"tenant_id"`
	Period          string              `json:"period"`
	TotalViolations int64               `json:"total_violations"`
	TotalRequests   int64               `json:"total_requests"`
	BlockRate       float64             `json:"block_rate_per_1k"`
	ByPIIType       []PIITypeStat       `json:"by_pii_type"`
	TopViolators    []UserViolationStat `json:"top_violators"`
	GeneratedAt     string              `json:"generated_at"`
}

type ComplianceDigestService struct{ pool *pgxpool.Pool }

func NewComplianceDigestService(pool *pgxpool.Pool) *ComplianceDigestService {
	return &ComplianceDigestService{pool: pool}
}

func (s *ComplianceDigestService) GetDigest(ctx context.Context, tenantID, yearMonth string) (*ComplianceDigest, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}

	digest := &ComplianceDigest{
		TenantID:    tenantID,
		Period:      yearMonth,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Total violations
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pii_violations
		WHERE tenant_id = $1
		  AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&digest.TotalViolations); err != nil {
		return nil, fmt.Errorf("digest total violations: %w", err)
	}

	// Total requests
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&digest.TotalRequests); err != nil {
		return nil, fmt.Errorf("digest total requests: %w", err)
	}

	if digest.TotalRequests > 0 {
		digest.BlockRate = float64(digest.TotalViolations) / float64(digest.TotalRequests) * 1000
	}

	// By PII type
	rows, err := s.pool.Query(ctx, `
		SELECT pii_type, COUNT(*) AS cnt
		FROM pii_violations
		WHERE tenant_id = $1
		  AND to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY pii_type ORDER BY cnt DESC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("digest by pii type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s PIITypeStat
		if err := rows.Scan(&s.PIIType, &s.Count); err != nil {
			return nil, err
		}
		digest.ByPIIType = append(digest.ByPIIType, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if digest.ByPIIType == nil {
		digest.ByPIIType = []PIITypeStat{}
	}

	// Top violators
	rows2, err := s.pool.Query(ctx, `
		SELECT v.user_id::text,
		       COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.department, ''),
		       COUNT(*) AS cnt
		FROM pii_violations v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.tenant_id = $1
		  AND to_char(v.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY v.user_id, u.name, u.email, u.department
		ORDER BY cnt DESC LIMIT 5
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("digest top violators: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var s UserViolationStat
		if err := rows2.Scan(&s.UserID, &s.UserName, &s.UserEmail, &s.Department, &s.Count); err != nil {
			return nil, err
		}
		digest.TopViolators = append(digest.TopViolators, s)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}
	if digest.TopViolators == nil {
		digest.TopViolators = []UserViolationStat{}
	}

	return digest, nil
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/admin && go test ./services/ -run TestMonthlyRiskScore -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/compliance_trend.go admin/services/compliance_trend_test.go
git commit -m "feat(compliance): RiskTrendService + ComplianceDigestService — monthly risk score history + period digest"
```

---

### Task 2: Compliance Report API Endpoints + Wire Main

**Files:**
- Create: `admin/api/compliance_reports.go`
- Create: `admin/api/compliance_reports_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing handler tests**

```go
// admin/api/compliance_reports_test.go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubRiskTrendSvc struct{}

func (s *stubRiskTrendSvc) GetRiskTrend(_ context.Context, tenantID string) (*services.RiskTrend, error) {
	return &services.RiskTrend{
		TenantID: tenantID,
		Points: []services.MonthlyRiskPoint{
			{Month: "2026-04", Violations: 10, Requests: 1000, RatePer1K: 10, RiskScore: 20},
			{Month: "2026-05", Violations: 5, Requests: 1000, RatePer1K: 5, RiskScore: 10},
		},
	}, nil
}

type stubDigestSvc struct{}

func (s *stubDigestSvc) GetDigest(_ context.Context, tenantID, _ string) (*services.ComplianceDigest, error) {
	return &services.ComplianceDigest{
		TenantID:        tenantID,
		TotalViolations: 15,
		TotalRequests:   2000,
		BlockRate:       7.5,
		ByPIIType:       []services.PIITypeStat{{PIIType: "email", Count: 10}},
		TopViolators:    []services.UserViolationStat{{UserID: "u1", Count: 8}},
	}, nil
}

func makeReportsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterComplianceReportRoutes(app, &stubRiskTrendSvc{}, &stubDigestSvc{})
	return app
}

func TestRiskTrend_OK(t *testing.T) {
	resp, err := makeReportsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/risk-trend", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.RiskTrend
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "t1", body.TenantID)
	assert.Len(t, body.Points, 2)
}

func TestComplianceDigest_OK(t *testing.T) {
	resp, err := makeReportsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/digest", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.ComplianceDigest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(15), body.TotalViolations)
}

func TestComplianceReports_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterComplianceReportRoutes(app, &stubRiskTrendSvc{}, &stubDigestSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/risk-trend", nil))
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./api/ -run 'TestRiskTrend|TestComplianceDigest|TestComplianceReports' -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 3: Implement handler**

```go
// admin/api/compliance_reports.go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type RiskTrendServiceIface interface {
	GetRiskTrend(ctx context.Context, tenantID string) (*services.RiskTrend, error)
}

type ComplianceDigestServiceIface interface {
	GetDigest(ctx context.Context, tenantID, yearMonth string) (*services.ComplianceDigest, error)
}

func RegisterComplianceReportRoutes(r fiber.Router, trendSvc RiskTrendServiceIface, digestSvc ComplianceDigestServiceIface) {
	r.Get("/api/admin/compliance/risk-trend", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := trendSvc.GetRiskTrend(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/compliance/digest", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := digestSvc.GetDigest(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})
}
```

- [ ] **Step 4: Wire into admin/main.go**

Read `admin/main.go`. After the compliance advanced services block, add:

```go
riskTrendSvc := services.NewRiskTrendService(pool)
complianceDigestSvc := services.NewComplianceDigestService(pool)
```

After `api.RegisterComplianceAdvancedRoutes(protected, ...)`:

```go
api.RegisterComplianceReportRoutes(protected, riskTrendSvc, complianceDigestSvc)
```

- [ ] **Step 5: Run all tests + build**

```bash
cd /path/to/worktree/admin && go test ./... -count=1
cd /path/to/worktree/admin && go build ./...
```

Expected: all PASS, no errors.

- [ ] **Step 6: Commit**

```bash
git add admin/api/compliance_reports.go admin/api/compliance_reports_test.go admin/main.go
git commit -m "feat(compliance): risk-trend + digest endpoints — monthly risk history and period summary"
```

---

### Task 3: Dashboard — Risk Trend Sparkline + Digest Card

**Files:**
- Modify: `dashboard/src/pages/admin/CompliancePage.tsx`

Read the full file before editing. Follow existing import and query patterns.

- [ ] **Step 1: TypeScript baseline**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit 2>&1 | head -5
```

Expected: 0 errors.

- [ ] **Step 2: Add two new useQuery calls**

Add alongside existing queries in `CompliancePage`:

```tsx
const { data: riskTrend } = useQuery({
  queryKey: ["compliance-risk-trend"],
  queryFn: () =>
    apiClient
      .get<{ points: RiskPoint[]; tenant_id: string }>("/api/admin/compliance/risk-trend")
      .then((r) => r.data),
});

const { data: digest } = useQuery({
  queryKey: ["compliance-digest"],
  queryFn: () =>
    apiClient
      .get<ComplianceDigest>("/api/admin/compliance/digest")
      .then((r) => r.data),
});
```

Add interfaces near the top of the file (or inline the types):

```tsx
interface RiskPoint {
  month: string;
  violations: number;
  requests: number;
  rate_per_1k: number;
  risk_score: number;
}

interface PIITypeStat {
  pii_type: string;
  count: number;
}

interface UserViolationStat {
  user_id: string;
  user_name: string;
  user_email: string;
  department: string;
  count: number;
}

interface ComplianceDigest {
  tenant_id: string;
  period: string;
  total_violations: number;
  total_requests: number;
  block_rate_per_1k: number;
  by_pii_type: PIITypeStat[];
  top_violators: UserViolationStat[];
}
```

- [ ] **Step 3: Add Risk Trend section to JSX**

Add after the existing compliance content (before `<BenchmarkCard />`):

```tsx
{/* Risk Score Trend */}
<div className="mt-8 border rounded-lg p-6">
  <h2 className="text-lg font-semibold mb-4">Risk Score Trend (Last 6 Months)</h2>
  {!riskTrend || riskTrend.points.length === 0 ? (
    <p className="text-gray-400 text-sm">No historical data yet.</p>
  ) : (
    <div>
      {/* Sparkline bars */}
      <div className="flex items-end gap-2 h-24 mb-3">
        {riskTrend.points.map((p) => {
          const height = Math.max(p.risk_score, 2);
          const color =
            p.risk_score >= 70
              ? "bg-red-500"
              : p.risk_score >= 40
              ? "bg-yellow-400"
              : "bg-green-500";
          return (
            <div key={p.month} className="flex-1 flex flex-col items-center gap-1">
              <span className="text-xs text-gray-500">{p.risk_score.toFixed(0)}</span>
              <div
                className={`w-full rounded-t ${color}`}
                style={{ height: `${height}%` }}
                title={`${p.month}: ${p.violations} violations / ${p.requests} requests`}
              />
            </div>
          );
        })}
      </div>
      <div className="flex gap-2">
        {riskTrend.points.map((p) => (
          <div key={p.month} className="flex-1 text-center text-xs text-gray-400">
            {p.month.slice(5)}
          </div>
        ))}
      </div>
      <div className="flex gap-4 mt-3 text-xs text-gray-500">
        <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-green-500 inline-block" /> Low (&lt;40)</span>
        <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-yellow-400 inline-block" /> Medium (40–70)</span>
        <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-red-500 inline-block" /> High (&gt;70)</span>
      </div>
    </div>
  )}
</div>

{/* Compliance Digest */}
<div className="mt-6 border rounded-lg p-6">
  <h2 className="text-lg font-semibold mb-4">
    Monthly Digest
    {digest && <span className="ml-2 text-sm font-normal text-gray-400">{digest.period}</span>}
  </h2>
  {!digest ? (
    <p className="text-gray-400 text-sm">Loading…</p>
  ) : (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      {/* Summary stats */}
      <div className="space-y-2">
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Total violations blocked</span>
          <span className="font-semibold">{digest.total_violations.toLocaleString()}</span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Total AI requests</span>
          <span className="font-semibold">{digest.total_requests.toLocaleString()}</span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Block rate</span>
          <span className="font-semibold">{digest.block_rate_per_1k.toFixed(2)} / 1K req</span>
        </div>
        {/* PII type breakdown */}
        {digest.by_pii_type.length > 0 && (
          <div className="mt-3">
            <p className="text-xs text-gray-500 mb-2 font-medium uppercase tracking-wide">By PII Type</p>
            {digest.by_pii_type.map((t) => (
              <div key={t.pii_type} className="flex justify-between text-sm py-0.5">
                <span className="text-gray-600 font-mono text-xs">{t.pii_type}</span>
                <span>{t.count}</span>
              </div>
            ))}
          </div>
        )}
      </div>
      {/* Top violators */}
      <div>
        <p className="text-xs text-gray-500 mb-2 font-medium uppercase tracking-wide">Top Violators</p>
        {digest.top_violators.length === 0 ? (
          <p className="text-sm text-gray-400">None this period</p>
        ) : (
          digest.top_violators.map((v, i) => (
            <div key={v.user_id} className="flex items-center justify-between py-1.5 border-b last:border-0">
              <div>
                <span className="text-sm font-medium">{v.user_name || v.user_email || v.user_id}</span>
                {v.department && (
                  <span className="ml-2 text-xs text-gray-400">{v.department}</span>
                )}
              </div>
              <span className={`text-sm font-semibold ${i === 0 ? "text-red-600" : "text-gray-700"}`}>
                {v.count}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  )}
</div>
```

- [ ] **Step 4: TypeScript check**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit
```

Fix any errors before committing.

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/admin/CompliancePage.tsx
git commit -m "feat(compliance): risk trend sparkline + monthly digest card on CompliancePage"
```

---

## Self-Review

**Spec coverage:**
- ✅ `MonthlyRiskScore` pure fn (0–100, saturates at 50/1k) — Task 1
- ✅ `RiskTrendService.GetRiskTrend` — 6-month monthly data, FULL OUTER JOIN violations+requests — Task 1
- ✅ `ComplianceDigestService.GetDigest` — total violations, requests, block rate, by PII type, top 5 violators — Task 1
- ✅ GET `/api/admin/compliance/risk-trend` — Task 2
- ✅ GET `/api/admin/compliance/digest?month=YYYY-MM` — Task 2
- ✅ Admin-only guard on both endpoints — Task 2
- ✅ Risk trend sparkline (bar chart, color-coded by risk level) — Task 3
- ✅ Digest card (stats + PII type breakdown + top violators) — Task 3
- ✅ No new migration required — all computed from existing tables

**Placeholder scan:** None.

**Type consistency:**
- `RiskTrendServiceIface.GetRiskTrend` matches `RiskTrendService.GetRiskTrend` exactly.
- `ComplianceDigestServiceIface.GetDigest` matches `ComplianceDigestService.GetDigest` exactly.
- Dashboard `RiskPoint` fields match Go `MonthlyRiskPoint` JSON tags.
- Dashboard `ComplianceDigest` fields match Go struct JSON tags.
