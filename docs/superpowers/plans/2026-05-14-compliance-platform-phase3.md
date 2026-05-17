# Compliance Platform Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add rule-based anomaly detection (violation spike alerts per user) and cross-tenant compliance benchmark (anonymized violation-rate percentiles across all tenants).

**Architecture:** Two new admin services with pure-function cores that are unit-testable without a DB. Anomaly service queries `pii_violations` and `usage_records` for the calling tenant only. Benchmark service aggregates across all tenants (no PII exposed — only percentile statistics). Both expose read-only GET endpoints. Dashboard gets a new Anomaly Alerts page and a benchmark comparison section on the existing CompliancePage.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5, React 19 + TanStack Query v5 + Tailwind v4

---

## Codebase Reference

- `admin/services/compliance.go` — patterns for DB queries against `pii_violations`; `ComputeRiskScore` shows pure-function pattern.
- `admin/api/compliance.go` — `currentYearMonth()` defined here (same package — do not redefine).
- `admin/main.go` — wire new services after `checklistSvc`.
- `infra/postgres/001_init.sql` — `usage_records` schema; timestamp column is `request_at`.
- `infra/postgres/015_pii_violations.sql` — `pii_violations` schema; timestamp is `occurred_at`.
- JWT claims: `c.Locals("claims").(*services.Claims)` → `TenantID`, `Role`. Admin-only: `role != "admin"` → 403.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `admin/services/anomaly.go` | Create | `IsAnomaly`, `AnomalySeverity` pure fns + `AnomalyService.GetAnomalies` |
| `admin/services/anomaly_test.go` | Create | Unit tests for pure functions |
| `admin/services/compliance_benchmark.go` | Create | `ComplianceBenchmark`, `GetComplianceBenchmark` cross-tenant percentiles |
| `admin/services/compliance_benchmark_test.go` | Create | Unit test for `PercentileRank` pure fn |
| `admin/api/compliance_advanced.go` | Create | `RegisterComplianceAdvancedRoutes` — 2 GET endpoints |
| `admin/api/compliance_advanced_test.go` | Create | Handler tests with stubs |
| `admin/main.go` | Modify | Wire `AnomalyService` + `ComplianceBenchmarkService` |
| `dashboard/src/pages/admin/ComplianceAnomalyPage.tsx` | Create | Anomaly alerts list |
| `dashboard/src/pages/admin/CompliancePage.tsx` | Modify | Add benchmark comparison card |
| `dashboard/src/App.tsx` | Modify | Add anomaly route |
| `dashboard/src/components/Layout.tsx` | Modify | Add "Anomaly Alerts" nav item |

---

## Task 1: Anomaly Detection Service (TDD)

**Files:**
- Create: `admin/services/anomaly.go`
- Create: `admin/services/anomaly_test.go`

**Algorithm:** For each user in the calling tenant, compare violations in the last 7 days vs the 4-week rolling weekly baseline. Flag as anomaly if `recentCount >= 5` AND (`baseline < 1` OR `recent > baseline * 3`). Severity is `"high"` if `recent > baseline * 5` or it's a first-time spike.

- [ ] **Step 1: Write failing unit tests**

Create `admin/services/anomaly_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestIsAnomaly_BelowMinThreshold(t *testing.T) {
	assert.False(t, services.IsAnomaly(3, 0))   // < 5 recent → not anomaly
	assert.False(t, services.IsAnomaly(4, 0))
}

func TestIsAnomaly_FirstTimeSpike(t *testing.T) {
	assert.True(t, services.IsAnomaly(5, 0))    // ≥ 5, no baseline → anomaly
	assert.True(t, services.IsAnomaly(10, 0.5)) // ≥ 5, baseline < 1 → anomaly
}

func TestIsAnomaly_SpikeOverBaseline(t *testing.T) {
	assert.True(t, services.IsAnomaly(15, 4))   // 15 > 4*3=12 → anomaly
	assert.False(t, services.IsAnomaly(10, 4))  // 10 < 4*3=12 → normal
}

func TestIsAnomaly_NormalRate(t *testing.T) {
	assert.False(t, services.IsAnomaly(6, 3))   // 6 < 3*3=9 → normal
}

func TestAnomalySeverity_High_FirstTime(t *testing.T) {
	assert.Equal(t, "high", services.AnomalySeverity(5, 0))
}

func TestAnomalySeverity_High_LargeSpike(t *testing.T) {
	assert.Equal(t, "high", services.AnomalySeverity(25, 4)) // 25 > 4*5=20
}

func TestAnomalySeverity_Medium(t *testing.T) {
	assert.Equal(t, "medium", services.AnomalySeverity(15, 4)) // 15 > 4*3 but ≤ 4*5
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestIsAnomaly|TestAnomalySeverity' -v
```

Expected: compile error — `services.IsAnomaly` not defined.

- [ ] **Step 3: Implement anomaly service**

Create `admin/services/anomaly.go`:

```go
package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	anomalyMinViolations    = 5
	anomalySpikeMultiplier  = 3.0
	anomalyHighMultiplier   = 5.0
	anomalyBaselineMinimum  = 1.0
)

// IsAnomaly returns true when recentCount represents a spike over weeklyBaseline.
// Requires at least anomalyMinViolations and either no prior history or > 3x baseline.
func IsAnomaly(recentCount int, weeklyBaseline float64) bool {
	if recentCount < anomalyMinViolations {
		return false
	}
	if weeklyBaseline < anomalyBaselineMinimum {
		return true
	}
	return float64(recentCount) > weeklyBaseline*anomalySpikeMultiplier
}

// AnomalySeverity classifies an anomaly as "high" or "medium".
// High: first-time spike or > 5x baseline. Medium: 3–5x baseline.
func AnomalySeverity(recentCount int, weeklyBaseline float64) string {
	if weeklyBaseline < anomalyBaselineMinimum || float64(recentCount) > weeklyBaseline*anomalyHighMultiplier {
		return "high"
	}
	return "medium"
}

// AnomalyUser is a user flagged as having an anomalous violation pattern.
type AnomalyUser struct {
	UserID          string  `json:"user_id"`
	UserName        string  `json:"user_name"`
	UserEmail       string  `json:"user_email"`
	Department      string  `json:"department"`
	RecentCount     int     `json:"recent_count"`
	WeeklyBaseline  float64 `json:"weekly_baseline"`
	Severity        string  `json:"severity"`
	DetectedAt      string  `json:"detected_at"`
}

// AnomalyService detects violation spikes per user within a tenant.
type AnomalyService struct {
	pool *pgxpool.Pool
}

func NewAnomalyService(pool *pgxpool.Pool) *AnomalyService {
	return &AnomalyService{pool: pool}
}

// GetAnomalies returns users with anomalous violation counts for the given tenant.
// Compares last 7 days vs 30-day rolling weekly average.
func (s *AnomalyService) GetAnomalies(ctx context.Context, tenantID string) ([]*AnomalyUser, error) {
	rows, err := s.pool.Query(ctx, `
		WITH recent AS (
			SELECT user_id, COUNT(*) AS recent_count
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '7 days'
			GROUP BY user_id
		),
		baseline AS (
			SELECT user_id, COUNT(*)::float / 4.0 AS weekly_avg
			FROM pii_violations
			WHERE tenant_id = $1
			  AND occurred_at >= NOW() - INTERVAL '30 days'
			GROUP BY user_id
		)
		SELECT r.user_id::text,
		       COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.department, ''),
		       r.recent_count,
		       COALESCE(b.weekly_avg, 0)
		FROM recent r
		LEFT JOIN baseline b ON b.user_id = r.user_id
		LEFT JOIN users u ON u.id = r.user_id
		WHERE r.recent_count >= $2
		  AND (b.weekly_avg IS NULL OR b.weekly_avg < 1 OR r.recent_count > b.weekly_avg * $3)
		ORDER BY r.recent_count DESC`,
		tenantID, anomalyMinViolations, anomalySpikeMultiplier,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var result []*AnomalyUser
	for rows.Next() {
		a := &AnomalyUser{DetectedAt: now}
		if err := rows.Scan(&a.UserID, &a.UserName, &a.UserEmail, &a.Department,
			&a.RecentCount, &a.WeeklyBaseline); err != nil {
			return nil, err
		}
		a.Severity = AnomalySeverity(a.RecentCount, a.WeeklyBaseline)
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []*AnomalyUser{}
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestIsAnomaly|TestAnomalySeverity' -v
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/anomaly.go admin/services/anomaly_test.go
git commit -m "feat(compliance): anomaly detection service — spike detection 7d vs 30d baseline"
```

---

## Task 2: Cross-Tenant Compliance Benchmark Service (TDD)

**Files:**
- Create: `admin/services/compliance_benchmark.go`
- Create: `admin/services/compliance_benchmark_test.go`

**Design:** Computes each tenant's violation rate (violations per 1000 requests) for the last 30 days, then returns Postgres `PERCENTILE_CONT` aggregates across all tenants. The calling tenant's rate and its percentile rank are included. No individual tenant data is exposed — only aggregate statistics.

- [ ] **Step 1: Write failing unit test for pure function**

Create `admin/services/compliance_benchmark_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestPercentileRank_Middle(t *testing.T) {
	// 2 tenants below, 2 above, 1 equal → rank ~40%
	rank := services.PercentileRank(5.0, []float64{1.0, 3.0, 5.0, 7.0, 9.0})
	assert.InDelta(t, 40.0, rank, 1.0)
}

func TestPercentileRank_Lowest(t *testing.T) {
	rank := services.PercentileRank(1.0, []float64{1.0, 5.0, 10.0})
	assert.InDelta(t, 0.0, rank, 0.1)
}

func TestPercentileRank_Highest(t *testing.T) {
	rank := services.PercentileRank(10.0, []float64{1.0, 5.0, 10.0})
	assert.InDelta(t, 66.7, rank, 1.0)
}

func TestPercentileRank_EmptySlice(t *testing.T) {
	rank := services.PercentileRank(5.0, []float64{})
	assert.Equal(t, 0.0, rank)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestPercentileRank' -v
```

Expected: compile error — `services.PercentileRank` not defined.

- [ ] **Step 3: Implement compliance benchmark service**

Create `admin/services/compliance_benchmark.go`:

```go
package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PercentileRank returns the percentage of values in all that are strictly less than value.
// Returns 0 if all is empty.
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

// ComplianceBenchmark holds the benchmark comparison for one tenant.
type ComplianceBenchmark struct {
	YourRate       float64 `json:"your_rate"`        // violations per 1000 requests
	P25            float64 `json:"p25"`
	P50            float64 `json:"p50"`
	P75            float64 `json:"p75"`
	TenantCount    int     `json:"tenant_count"`
	PercentileRank float64 `json:"percentile_rank"`  // lower is better for violations
	InsufficientData bool  `json:"insufficient_data"`
}

// ComplianceBenchmarkService computes cross-tenant violation rate benchmarks.
type ComplianceBenchmarkService struct {
	pool *pgxpool.Pool
}

func NewComplianceBenchmarkService(pool *pgxpool.Pool) *ComplianceBenchmarkService {
	return &ComplianceBenchmarkService{pool: pool}
}

// GetComplianceBenchmark returns anonymized violation rate percentiles across all tenants
// and the calling tenant's position within that distribution.
func (s *ComplianceBenchmarkService) GetComplianceBenchmark(ctx context.Context, tenantID string) (*ComplianceBenchmark, error) {
	// Compute per-tenant violation rates: violations / requests * 1000 (last 30 days)
	rows, err := s.pool.Query(ctx, `
		SELECT sub.tenant_id, sub.rate
		FROM (
			SELECT v.tenant_id,
			       COUNT(v.id)::float / NULLIF(r.total_requests, 0) * 1000 AS rate
			FROM (
				SELECT tenant_id, COUNT(*) AS id
				FROM pii_violations
				WHERE occurred_at >= NOW() - INTERVAL '30 days'
				GROUP BY tenant_id
			) v
			JOIN (
				SELECT tenant_id, COUNT(*) AS total_requests
				FROM usage_records
				WHERE request_at >= NOW() - INTERVAL '30 days'
				GROUP BY tenant_id
			) r ON r.tenant_id = v.tenant_id
		) sub`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allRates []float64
	yourRate := 0.0
	for rows.Next() {
		var tid string
		var rate float64
		if err := rows.Scan(&tid, &rate); err != nil {
			return nil, err
		}
		allRates = append(allRates, rate)
		if tid == tenantID {
			yourRate = rate
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const minTenants = 3
	if len(allRates) < minTenants {
		return &ComplianceBenchmark{
			YourRate:         yourRate,
			TenantCount:      len(allRates),
			InsufficientData: true,
		}, nil
	}

	// Compute percentiles using Postgres for accuracy
	var p25, p50, p75 float64
	err = s.pool.QueryRow(ctx, `
		SELECT
			PERCENTILE_CONT(0.25) WITHIN GROUP (ORDER BY rate),
			PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rate),
			PERCENTILE_CONT(0.75) WITHIN GROUP (ORDER BY rate)
		FROM (
			SELECT COUNT(v.id)::float / NULLIF(r.total_requests, 0) * 1000 AS rate
			FROM (
				SELECT tenant_id, COUNT(*) AS id FROM pii_violations
				WHERE occurred_at >= NOW() - INTERVAL '30 days' GROUP BY tenant_id
			) v
			JOIN (
				SELECT tenant_id, COUNT(*) AS total_requests FROM usage_records
				WHERE request_at >= NOW() - INTERVAL '30 days' GROUP BY tenant_id
			) r ON r.tenant_id = v.tenant_id
		) sub`,
	).Scan(&p25, &p50, &p75)
	if err != nil {
		return nil, err
	}

	return &ComplianceBenchmark{
		YourRate:       yourRate,
		P25:            p25,
		P50:            p50,
		P75:            p75,
		TenantCount:    len(allRates),
		PercentileRank: PercentileRank(yourRate, allRates),
	}, nil
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestPercentileRank' -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/compliance_benchmark.go admin/services/compliance_benchmark_test.go
git commit -m "feat(compliance): cross-tenant benchmark service — violation-rate percentiles across tenants"
```

---

## Task 3: Advanced Compliance API Routes + Wire Main

**Files:**
- Create: `admin/api/compliance_advanced.go`
- Create: `admin/api/compliance_advanced_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing handler tests**

Create `admin/api/compliance_advanced_test.go`:

```go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubAnomalySvc struct{}

func (s *stubAnomalySvc) GetAnomalies(_ context.Context, _ string) ([]*services.AnomalyUser, error) {
	return []*services.AnomalyUser{
		{UserID: "u1", UserName: "Alice", Severity: "high", RecentCount: 10},
	}, nil
}

type stubBenchmarkSvc struct{}

func (s *stubBenchmarkSvc) GetComplianceBenchmark(_ context.Context, _ string) (*services.ComplianceBenchmark, error) {
	return &services.ComplianceBenchmark{YourRate: 5.0, P50: 3.0, TenantCount: 10}, nil
}

func setupAdvancedComplianceApp(aSvc api.AnomalyServiceIface, bSvc api.ComplianceBenchmarkServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterComplianceAdvancedRoutes(app, aSvc, bSvc)
	return app
}

func TestGetAnomalies_OK(t *testing.T) {
	app := setupAdvancedComplianceApp(&stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/anomalies", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetComplianceBenchmark_OK(t *testing.T) {
	app := setupAdvancedComplianceApp(&stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/benchmark", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestComplianceAdvanced_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterComplianceAdvancedRoutes(app, &stubAnomalySvc{}, &stubBenchmarkSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/anomalies", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./api/ -run 'TestGetAnomalies|TestGetCompliance|TestComplianceAdvanced' -v
```

Expected: compile error — `api.RegisterComplianceAdvancedRoutes` not defined.

- [ ] **Step 3: Implement the API**

Create `admin/api/compliance_advanced.go`:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type AnomalyServiceIface interface {
	GetAnomalies(ctx context.Context, tenantID string) ([]*services.AnomalyUser, error)
}

type ComplianceBenchmarkServiceIface interface {
	GetComplianceBenchmark(ctx context.Context, tenantID string) (*services.ComplianceBenchmark, error)
}

func RegisterComplianceAdvancedRoutes(app fiber.Router, anomalySvc AnomalyServiceIface, benchmarkSvc ComplianceBenchmarkServiceIface) {
	app.Get("/api/admin/compliance/anomalies", getComplianceAnomalies(anomalySvc))
	app.Get("/api/admin/compliance/benchmark", getComplianceBenchmark(benchmarkSvc))
}

func getComplianceAnomalies(svc AnomalyServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		anomalies, err := svc.GetAnomalies(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"anomalies": anomalies})
	}
}

func getComplianceBenchmark(svc ComplianceBenchmarkServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		bm, err := svc.GetComplianceBenchmark(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(bm)
	}
}
```

- [ ] **Step 4: Wire into admin/main.go**

After `checklistSvc := services.NewChecklistService(pool)`, add:

```go
anomalySvc := services.NewAnomalyService(pool)
complianceBenchmarkSvc := services.NewComplianceBenchmarkService(pool)
```

After `api.RegisterChecklistRoutes(protected, checklistSvc)`, add:

```go
api.RegisterComplianceAdvancedRoutes(protected, anomalySvc, complianceBenchmarkSvc)
```

- [ ] **Step 5: Run all admin tests**

```bash
cd /path/to/worktree/admin
go test ./... -count=1
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add admin/api/compliance_advanced.go admin/api/compliance_advanced_test.go admin/main.go
git commit -m "feat(compliance): advanced routes — GET /anomalies, GET /benchmark; wire AnomalyService + ComplianceBenchmarkService"
```

---

## Task 4: Dashboard — Anomaly Alerts Page + Benchmark Card

**Files:**
- Create: `dashboard/src/pages/admin/ComplianceAnomalyPage.tsx`
- Modify: `dashboard/src/pages/admin/CompliancePage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

- [ ] **Step 1: Create ComplianceAnomalyPage.tsx**

Create `dashboard/src/pages/admin/ComplianceAnomalyPage.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface AnomalyUser {
  user_id: string;
  user_name: string;
  user_email: string;
  department: string;
  recent_count: number;
  weekly_baseline: number;
  severity: "high" | "medium";
  detected_at: string;
}

const SEVERITY_COLORS = {
  high: "text-red-700 bg-red-50 border-red-200",
  medium: "text-yellow-700 bg-yellow-50 border-yellow-200",
};

export default function ComplianceAnomalyPage() {
  const { data, isLoading } = useQuery({
    queryKey: ["compliance-anomalies"],
    queryFn: () =>
      apiClient
        .get<{ anomalies: AnomalyUser[] }>("/api/admin/compliance/anomalies")
        .then((r) => r.data.anomalies),
    refetchInterval: 5 * 60 * 1000, // refresh every 5 minutes
  });

  if (isLoading) return <div className="p-8 text-gray-400">Detecting anomalies…</div>;

  const anomalies = data ?? [];

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-1">Anomaly Alerts</h1>
      <p className="text-gray-500 text-sm mb-6">
        Users with violation counts significantly above their 30-day baseline (last 7 days vs 4-week rolling average).
      </p>

      {anomalies.length === 0 ? (
        <div className="px-6 py-12 text-center text-gray-400 border rounded-lg">
          No anomalies detected in the last 7 days
        </div>
      ) : (
        <div className="space-y-3">
          {anomalies.map((a) => (
            <div
              key={a.user_id}
              className={`border rounded-lg p-4 ${SEVERITY_COLORS[a.severity]}`}
            >
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-semibold text-gray-800">{a.user_name || a.user_email}</span>
                    <span className="text-xs px-2 py-0.5 rounded-full border font-medium uppercase">
                      {a.severity}
                    </span>
                  </div>
                  <p className="text-sm text-gray-600">{a.department}</p>
                </div>
                <div className="text-right">
                  <div className="text-2xl font-bold text-gray-800">{a.recent_count}</div>
                  <div className="text-xs text-gray-500">violations (7d)</div>
                </div>
              </div>
              <div className="mt-3 flex gap-6 text-sm text-gray-600">
                <span>
                  Baseline:{" "}
                  <span className="font-medium">
                    {a.weekly_baseline < 1 ? "none" : a.weekly_baseline.toFixed(1)}/week
                  </span>
                </span>
                <span>
                  Detected: <span className="font-medium">{new Date(a.detected_at).toLocaleDateString()}</span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Add benchmark card to CompliancePage.tsx**

Read the current `dashboard/src/pages/admin/CompliancePage.tsx`. Find the bottom of the JSX and add a benchmark comparison card before the closing `</div>`:

```tsx
{/* Industry Benchmark */}
<BenchmarkCard />
```

Add this component definition at the top of the same file (before the main component), after the existing imports:

```tsx
function BenchmarkCard() {
  const { data } = useQuery({
    queryKey: ["compliance-benchmark"],
    queryFn: () =>
      apiClient
        .get<{
          your_rate: number;
          p25: number;
          p50: number;
          p75: number;
          tenant_count: number;
          percentile_rank: number;
          insufficient_data: boolean;
        }>("/api/admin/compliance/benchmark")
        .then((r) => r.data),
  });

  if (!data) return null;
  if (data.insufficient_data) {
    return (
      <div className="mt-8 p-4 bg-gray-50 rounded-lg border text-sm text-gray-500">
        Industry benchmark not yet available (requires data from 3+ tenants).
      </div>
    );
  }

  const betterThan = (100 - data.percentile_rank).toFixed(0);

  return (
    <div className="mt-8 border rounded-lg p-6">
      <h2 className="text-lg font-semibold mb-4">Industry Benchmark</h2>
      <p className="text-sm text-gray-500 mb-4">
        Violation rate (violations per 1,000 AI requests, last 30 days) — lower is better.
        Based on {data.tenant_count} anonymised tenants.
      </p>
      <div className="grid grid-cols-4 gap-4 mb-4">
        {[
          { label: "Your Rate", value: data.your_rate.toFixed(1), highlight: true },
          { label: "Industry P25", value: data.p25.toFixed(1) },
          { label: "Industry P50", value: data.p50.toFixed(1) },
          { label: "Industry P75", value: data.p75.toFixed(1) },
        ].map(({ label, value, highlight }) => (
          <div
            key={label}
            className={`p-3 rounded-lg text-center ${highlight ? "bg-blue-50 border border-blue-100" : "bg-gray-50"}`}
          >
            <div className={`text-2xl font-bold ${highlight ? "text-blue-700" : "text-gray-800"}`}>
              {value}
            </div>
            <div className="text-xs text-gray-500 mt-1">{label}</div>
          </div>
        ))}
      </div>
      <p className="text-sm text-gray-600">
        Your violation rate is better than{" "}
        <span className="font-semibold text-green-600">{betterThan}%</span> of comparable tenants.
      </p>
    </div>
  );
}
```

Make sure `apiClient` is imported from `../../api/client` — check existing imports.

- [ ] **Step 3: Add route to App.tsx**

Add import after `ComplianceChecklistPage`:

```tsx
import ComplianceAnomalyPage from "./pages/admin/ComplianceAnomalyPage";
```

Add route after `admin/compliance/checklist`:

```tsx
<Route path="admin/compliance/anomalies" element={<ProtectedRoute adminOnly><ComplianceAnomalyPage /></ProtectedRoute>} />
```

- [ ] **Step 4: Add nav item to Layout.tsx**

In `adminNavItems`, after `{ label: "EU AI Act", href: "/admin/compliance/checklist" }`:

```tsx
{ label: "Anomaly Alerts", href: "/admin/compliance/anomalies" },
```

- [ ] **Step 5: TypeScript check**

```bash
cd /path/to/worktree/dashboard
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/admin/ComplianceAnomalyPage.tsx \
        dashboard/src/pages/admin/CompliancePage.tsx \
        dashboard/src/App.tsx \
        dashboard/src/components/Layout.tsx
git commit -m "feat(compliance): anomaly alerts page + industry benchmark card on CompliancePage"
```

---

## Self-Review

**Spec coverage:**
- ✅ 异常行为检测 — Tasks 1, 3, 4 (spike detection per user, 7d vs 30d rolling baseline)
- ✅ 跨公司合规 benchmark — Tasks 2, 3, 4 (anonymised violation-rate percentiles, insufficient-data guard)
- 合规咨询 marketplace — deferred (requires external integrations + billing)
- 监管动态订阅 — deferred (low engineering complexity, high content/ops complexity)

**Placeholder scan:** none found.

**Type consistency:**
- `AnomalyUser` defined in `anomaly.go`, used in `AnomalyServiceIface` and `ComplianceAnomalyPage` interface.
- `ComplianceBenchmark` defined in `compliance_benchmark.go`, used in `ComplianceBenchmarkServiceIface` and `BenchmarkCard` interface.
- `PercentileRank(value float64, all []float64) float64` — pure fn exported, tested, and used in `GetComplianceBenchmark`. ✅
