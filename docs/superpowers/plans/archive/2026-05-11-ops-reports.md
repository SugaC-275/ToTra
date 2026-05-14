# Ops Reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three admin-only operational reporting features to ToTra: department cost chargeback with CSV export, linear month-end budget forecast, and identification of low-activity employees.

**Architecture:** Service methods are added to the existing `UsageService` in `admin/services/usage.go` and exposed via a new registration function `RegisterUsageAdminRoutes` in `admin/api/usage.go`. A single new React page (`DepartmentReportPage`) handles Feature 1 (department report + CSV). Features 2 and 3 are surfaced directly on the existing `DashboardPage` as new cards. All three features are admin-only, enforced in both the Go handlers (role check) and in React (`<ProtectedRoute adminOnly>`). The new API routes are wired into `admin/main.go` by reusing the same `usageSvc` instance that already serves `/api/usage/summary` and `/api/usage/adoption`.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5 (`pgtype.Timestamptz` for nullable timestamps), React 19 + TypeScript 6, TanStack Query v5, Tailwind v4, local shadcn-style UI components (`Card`, `Button` in `dashboard/src/components/ui/`), React Router v6.

---

## File Map

| Task | Files |
|------|-------|
| 1 | `admin/services/usage.go` (modify) |
| 2 | `admin/api/usage.go` (modify), `admin/main.go` (modify) |
| 3 | `dashboard/src/api/client.ts` (modify) |
| 4 | `dashboard/src/pages/admin/DepartmentReportPage.tsx` (create) |
| 5 | `dashboard/src/pages/admin/DashboardPage.tsx` (modify), `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 1: Service layer — three new methods on UsageService

**Files:**
- Modify: `admin/services/usage.go`

The existing file imports only `context` and `github.com/jackc/pgx/v5/pgxpool`. You will extend it with three new structs, three new methods on `*UsageService`, and expand `UsageServiceInterface` with the new method signatures.

- [ ] **Step 1: Add the three new structs to `admin/services/usage.go`**

After the closing brace of the `AdoptionStats` struct (line 21), insert:

```go
type DeptSummary struct {
	Department   string  `json:"department"`
	UserCount    int     `json:"user_count"`
	ActiveUsers  int     `json:"active_users"`
	TotalSCU     float64 `json:"total_scu"`
	TotalUSD     float64 `json:"total_usd"`
	RequestCount int     `json:"request_count"`
}

type BudgetForecast struct {
	Month         string  `json:"month"`
	CurrentSCU    float64 `json:"current_scu"`
	CurrentUSD    float64 `json:"current_usd"`
	DaysElapsed   int     `json:"days_elapsed"`
	DaysInMonth   int     `json:"days_in_month"`
	ProjectedSCU  float64 `json:"projected_scu"`
	ProjectedUSD  float64 `json:"projected_usd"`
	PriorMonthSCU float64 `json:"prior_month_scu"`
	TrendPct      float64 `json:"trend_pct"`
}

type InactiveUser struct {
	UserID       string  `json:"user_id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Department   string  `json:"department"`
	JobRole      string  `json:"job_role"`
	ActiveDays   int     `json:"active_days"`
	LastActiveAt *string `json:"last_active_at"` // "2006-01-02" or null
}
```

- [ ] **Step 2: Expand `UsageServiceInterface`**

Replace the existing interface block (lines 23–26) with:

```go
type UsageServiceInterface interface {
	GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error)
	GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error)
	GetDepartmentSummary(ctx context.Context, tenantID, yearMonth string) ([]*DeptSummary, error)
	GetBudgetForecast(ctx context.Context, tenantID, yearMonth string) (*BudgetForecast, error)
	GetInactiveUsers(ctx context.Context, tenantID, yearMonth string, maxDays int) ([]*InactiveUser, error)
}
```

- [ ] **Step 3: Update the import block in `admin/services/usage.go`**

The file currently imports only `context` and `pgxpool`. Replace with:

```go
import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)
```

`pgtype` is already an indirect dependency via `pgx/v5` in `go.mod` — no `go get` required.

- [ ] **Step 4: Add `GetDepartmentSummary` at the end of `admin/services/usage.go`**

```go
func (s *UsageService) GetDepartmentSummary(ctx context.Context, tenantID, yearMonth string) ([]*DeptSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(u.department, '(未设置)') AS department,
			COUNT(DISTINCT u.id)               AS user_count,
			COUNT(DISTINCT r.user_id)          AS active_users,
			COALESCE(SUM(r.scu_cost), 0)       AS total_scu,
			COALESCE(SUM(r.usd_cost), 0)       AS total_usd,
			COUNT(r.id)                        AS request_count
		FROM users u
		LEFT JOIN usage_records r
			ON r.user_id = u.id
			AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true
		GROUP BY COALESCE(u.department, '(未设置)')
		ORDER BY total_scu DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*DeptSummary
	for rows.Next() {
		d := &DeptSummary{}
		if err := rows.Scan(&d.Department, &d.UserCount, &d.ActiveUsers,
			&d.TotalSCU, &d.TotalUSD, &d.RequestCount); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
```

- [ ] **Step 5: Add `GetBudgetForecast` at the end of `admin/services/usage.go`**

```go
func (s *UsageService) GetBudgetForecast(ctx context.Context, tenantID, yearMonth string) (*BudgetForecast, error) {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, err
	}
	daysInMonth := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()

	now := time.Now().UTC()
	daysElapsed := daysInMonth // past month: use full month
	if yearMonth == now.Format("2006-01") {
		daysElapsed = now.Day()
	}

	var currentSCU, currentUSD float64
	s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(scu_cost),0), COALESCE(SUM(usd_cost),0)
		 FROM usage_records WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2`,
		tenantID, yearMonth).Scan(&currentSCU, &currentUSD)

	priorMonth := t.AddDate(0, -1, 0).Format("2006-01")
	var priorMonthSCU float64
	s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(scu_cost),0)
		 FROM usage_records WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2`,
		tenantID, priorMonth).Scan(&priorMonthSCU)

	var projSCU, projUSD float64
	if daysElapsed > 0 {
		projSCU = currentSCU / float64(daysElapsed) * float64(daysInMonth)
		projUSD = currentUSD / float64(daysElapsed) * float64(daysInMonth)
	}

	var trendPct float64
	if priorMonthSCU > 0 {
		trendPct = (projSCU - priorMonthSCU) / priorMonthSCU * 100
	}

	return &BudgetForecast{
		Month:         yearMonth,
		CurrentSCU:    currentSCU,
		CurrentUSD:    currentUSD,
		DaysElapsed:   daysElapsed,
		DaysInMonth:   daysInMonth,
		ProjectedSCU:  projSCU,
		ProjectedUSD:  projUSD,
		PriorMonthSCU: priorMonthSCU,
		TrendPct:      trendPct,
	}, nil
}
```

- [ ] **Step 6: Add `GetInactiveUsers` at the end of `admin/services/usage.go`**

```go
func (s *UsageService) GetInactiveUsers(ctx context.Context, tenantID, yearMonth string, maxDays int) ([]*InactiveUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name, u.email,
		       COALESCE(u.department, ''),
		       COALESCE(u.job_role, ''),
		       COUNT(DISTINCT DATE(r.request_at)) AS active_days,
		       MAX(r.request_at)                  AS last_active_at
		FROM users u
		LEFT JOIN usage_records r
			ON r.user_id = u.id
			AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true AND u.role != 'admin'
		GROUP BY u.id, u.name, u.email, u.department, u.job_role
		HAVING COUNT(DISTINCT DATE(r.request_at)) < $3
		ORDER BY active_days ASC, u.name`,
		tenantID, yearMonth, maxDays,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*InactiveUser
	for rows.Next() {
		u := &InactiveUser{}
		var lastAt pgtype.Timestamptz
		if err := rows.Scan(&u.UserID, &u.Name, &u.Email,
			&u.Department, &u.JobRole, &u.ActiveDays, &lastAt); err != nil {
			return nil, err
		}
		if lastAt.Valid {
			s := lastAt.Time.Format("2006-01-02")
			u.LastActiveAt = &s
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
```

- [ ] **Step 7: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output (zero errors). If you see `pgtype: package not in GOROOT`, run `go mod tidy` first (it is already an indirect dep in `go.mod`).

- [ ] **Step 8: Commit**

```bash
git add admin/services/usage.go
git commit -m "feat(usage): add department summary, budget forecast, inactive users service methods"
```

---

## Task 2: API layer — new admin routes + wire into main.go

**Files:**
- Modify: `admin/api/usage.go`
- Modify: `admin/main.go`

The existing `admin/api/usage.go` imports only `fiber/v2` and `services`. You will add a second registration function `RegisterUsageAdminRoutes` plus four handler functions. The original `RegisterUsageRoutes` and its two handlers are left unchanged.

Note: `RegisterUsageAdminRoutes` takes `*services.UsageService` (the concrete type, not the interface) because the three new methods are not part of `UsageServiceInterface`. This is intentional and consistent with the pattern used in `admin/api/kpi.go` (`RegisterKPIRoutes` takes `*services.KPIService`).

- [ ] **Step 1: Extend the import block in `admin/api/usage.go`**

Replace the current two-import block with:

```go
import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)
```

- [ ] **Step 2: Add `RegisterUsageAdminRoutes` and four handlers at the end of `admin/api/usage.go`**

```go
func RegisterUsageAdminRoutes(app fiber.Router, svc *services.UsageService) {
	app.Get("/api/admin/usage/department", getDepartmentSummary(svc))
	app.Get("/api/admin/usage/department/export", exportDepartmentCSV(svc))
	app.Get("/api/admin/usage/forecast", getBudgetForecast(svc))
	app.Get("/api/admin/usage/inactive", getInactiveUsers(svc))
}

func getDepartmentSummary(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		summaries, err := svc.GetDepartmentSummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "departments": summaries})
	}
}

func exportDepartmentCSV(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		summaries, err := svc.GetDepartmentSummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		w.Write([]string{"Department", "Total Users", "Active Users", "SCU", "USD Cost", "Requests"})
		for _, d := range summaries {
			w.Write([]string{
				d.Department,
				strconv.Itoa(d.UserCount),
				strconv.Itoa(d.ActiveUsers),
				strconv.FormatFloat(d.TotalSCU, 'f', 2, 64),
				strconv.FormatFloat(d.TotalUSD, 'f', 4, 64),
				strconv.Itoa(d.RequestCount),
			})
		}
		w.Flush()
		c.Set("Content-Type", "text/csv; charset=utf-8")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cost-chargeback-%s.csv"`, month))
		return c.SendString(buf.String())
	}
}

func getBudgetForecast(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		forecast, err := svc.GetBudgetForecast(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(forecast)
	}
}

func getInactiveUsers(svc *services.UsageService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required"})
		}
		maxDays := 3
		if v := c.QueryInt("max_days", 3); v > 0 {
			maxDays = v
		}
		users, err := svc.GetInactiveUsers(c.Context(), claims.TenantID, month, maxDays)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "max_days": maxDays, "users": users})
	}
}
```

- [ ] **Step 3: Wire `RegisterUsageAdminRoutes` into `admin/main.go`**

Find line 46 in `admin/main.go`:

```go
api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
```

Replace it with:

```go
usageSvc := services.NewUsageService(pool)
api.RegisterUsageRoutes(protected, usageSvc)
api.RegisterUsageAdminRoutes(protected, usageSvc)
```

This ensures both the original interface-typed routes and the new concrete-typed admin routes share one `pgxpool` connection instance.

- [ ] **Step 4: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output.

- [ ] **Step 5: Run existing usage tests to confirm nothing regressed**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run TestGet -v
```

Expected output includes:

```
--- PASS: TestGetMonthlySummary
--- PASS: TestGetAdoptionRate
```

- [ ] **Step 6: Commit**

```bash
git add admin/api/usage.go admin/main.go
git commit -m "feat(api): add department report, budget forecast, inactive users endpoints"
```

---

## Task 3: Frontend API client — new types and functions

**Files:**
- Modify: `dashboard/src/api/client.ts`

The file currently ends at line 224 with `updateMyProfile`. Do not change any existing code. Add new interfaces and functions in the two locations described below.

- [ ] **Step 1: Insert three new interfaces after `AdoptionStats` (after line 88)**

Insert after `AdoptionStats` and before the `// ---- Phase 2 types ----` comment:

```typescript
export interface DeptSummary {
  department: string;
  user_count: number;
  active_users: number;
  total_scu: number;
  total_usd: number;
  request_count: number;
}

export interface BudgetForecast {
  month: string;
  current_scu: number;
  current_usd: number;
  days_elapsed: number;
  days_in_month: number;
  projected_scu: number;
  projected_usd: number;
  prior_month_scu: number;
  trend_pct: number;
}

export interface InactiveUser {
  user_id: string;
  name: string;
  email: string;
  department: string;
  job_role: string;
  active_days: number;
  last_active_at: string | null;
}
```

- [ ] **Step 2: Append four new API functions at the end of `dashboard/src/api/client.ts`**

After the final `updateMyProfile` function, add:

```typescript
// ---- Ops Reports ----

export const getDepartmentSummary = (month: string) =>
  apiClient.get<{ month: string; departments: DeptSummary[] }>(
    `/api/admin/usage/department?month=${month}`
  );

export const exportDepartmentCSV = async (month: string): Promise<void> => {
  const token = localStorage.getItem("totra_token");
  const res = await fetch(
    `${BASE_URL}/api/admin/usage/department/export?month=${month}`,
    { headers: { Authorization: `Bearer ${token ?? ""}` } }
  );
  if (!res.ok) throw new Error("Export failed");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `cost-chargeback-${month}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};

export const getBudgetForecast = (month: string) =>
  apiClient.get<BudgetForecast>(`/api/admin/usage/forecast?month=${month}`);

export const getInactiveUsers = (month: string, maxDays = 3) =>
  apiClient.get<{ month: string; max_days: number; users: InactiveUser[] }>(
    `/api/admin/usage/inactive?month=${month}&max_days=${maxDays}`
  );
```

`exportDepartmentCSV` uses raw `fetch` instead of `apiClient` because it needs a binary blob response. `BASE_URL` is already declared at the top of the file (`const BASE_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081"`).

- [ ] **Step 3: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/api/client.ts
git commit -m "feat(client): add department, forecast, inactive users API functions"
```

---

## Task 4: DepartmentReportPage

**Files:**
- Create: `dashboard/src/pages/admin/DepartmentReportPage.tsx`

This page is self-contained. It queries `getDepartmentSummary` (from Task 3), renders a summary table with department cost breakdown and percentage share, exposes a month picker, and has an Export CSV button that calls `exportDepartmentCSV`. It imports from the UI library already in `dashboard/src/components/ui/` (`Card`, `Button`).

- [ ] **Step 1: Create `dashboard/src/pages/admin/DepartmentReportPage.tsx`**

```tsx
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getDepartmentSummary, exportDepartmentCSV } from "../../api/client";
import type { DeptSummary } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";

const currentMonth = new Date().toISOString().slice(0, 7);

export function DepartmentReportPage() {
  const [month, setMonth] = useState(currentMonth);
  const [exporting, setExporting] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ["department-summary", month],
    queryFn: () => getDepartmentSummary(month).then((r) => r.data),
  });

  const departments: DeptSummary[] = data?.departments ?? [];
  const totalSCU = departments.reduce((s, d) => s + d.total_scu, 0);
  const totalUSD = departments.reduce((s, d) => s + d.total_usd, 0);

  async function handleExport() {
    setExporting(true);
    try {
      await exportDepartmentCSV(month);
    } finally {
      setExporting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Department Cost Report</h1>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
          />
          <Button
            variant="outline"
            onClick={handleExport}
            disabled={exporting || departments.length === 0}
          >
            {exporting ? "Exporting..." : "Export CSV"}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total SCU</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {totalSCU.toLocaleString(undefined, { maximumFractionDigits: 0 })}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Cost</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">${totalUSD.toFixed(2)}</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Cost by Department — {month}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : departments.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4 text-center">No data for {month}.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 px-4 font-medium">Department</th>
                  <th className="text-right py-2 font-medium">Users</th>
                  <th className="text-right py-2 font-medium">Active</th>
                  <th className="text-right py-2 font-medium">SCU</th>
                  <th className="text-right py-2 font-medium">Cost (USD)</th>
                  <th className="text-right py-2 px-4 font-medium">% of Total</th>
                </tr>
              </thead>
              <tbody>
                {departments.map((d) => (
                  <tr key={d.department} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                    <td className="py-2 px-4 font-medium">{d.department}</td>
                    <td className="py-2 text-right text-zinc-400">{d.user_count}</td>
                    <td className="py-2 text-right text-emerald-400">{d.active_users}</td>
                    <td className="py-2 text-right font-mono">
                      {d.total_scu.toLocaleString(undefined, { maximumFractionDigits: 0 })}
                    </td>
                    <td className="py-2 text-right font-mono">${d.total_usd.toFixed(2)}</td>
                    <td className="py-2 px-4 text-right text-zinc-400">
                      {totalSCU > 0 ? `${((d.total_scu / totalSCU) * 100).toFixed(1)}%` : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors. If you see `Cannot find module '../../api/client'` or missing exports, ensure Task 3 is complete.

- [ ] **Step 3: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/pages/admin/DepartmentReportPage.tsx
git commit -m "feat(dashboard): add Department Cost Report page with CSV export"
```

---

## Task 5: Wire DepartmentReportPage + add forecast card + inactive users to Dashboard

**Files:**
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`
- Modify: `dashboard/src/pages/admin/DashboardPage.tsx`

This task wires up the route and sidebar nav item for `DepartmentReportPage`, then enhances the existing `DashboardPage` with a 4th summary card (month-end forecast) and a conditional inactive-users table below the Usage Details card.

### 5a — App.tsx: add import and route

- [ ] **Step 1: Add import to `dashboard/src/App.tsx`**

After the existing `DashboardPage` import (line 6):

```typescript
import { DepartmentReportPage } from "./pages/admin/DepartmentReportPage";
```

- [ ] **Step 2: Add route inside the nested `<Route path="/">` in `dashboard/src/App.tsx`**

After the `admin/integrations` route (line 38):

```tsx
<Route path="admin/reports" element={<ProtectedRoute adminOnly><DepartmentReportPage /></ProtectedRoute>} />
```

### 5b — Layout.tsx: add sidebar nav item

- [ ] **Step 3: Insert Reports entry into `adminNavItems` in `dashboard/src/components/Layout.tsx`**

After `{ label: "KPI", href: "/admin/kpi" }`, add:

```typescript
{ label: "Reports", href: "/admin/reports" },
```

The full updated array:

```typescript
const adminNavItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Reports", href: "/admin/reports" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "My Usage", href: "/me" },
];
```

### 5c — DashboardPage.tsx: forecast card + inactive users

- [ ] **Step 4: Extend the import in `dashboard/src/pages/admin/DashboardPage.tsx`**

Replace the existing client import line with:

```typescript
import { getMonthlySummary, getAdoptionRate, getBudgetForecast, getInactiveUsers } from "../../api/client";
import type { BudgetForecast, InactiveUser } from "../../api/client";
```

- [ ] **Step 5: Add two new queries inside `DashboardPage`**

After the existing `adoptionData` query block (after line 17), add:

```typescript
const { data: forecastData } = useQuery({
  queryKey: ["budget-forecast", currentMonth],
  queryFn: () => getBudgetForecast(currentMonth).then((r) => r.data),
});

const { data: inactiveData } = useQuery({
  queryKey: ["inactive-users", currentMonth],
  queryFn: () => getInactiveUsers(currentMonth, 3).then((r) => r.data),
});

const forecast: BudgetForecast | undefined = forecastData;
const inactiveUsers: InactiveUser[] = inactiveData?.users ?? [];
```

- [ ] **Step 6: Replace the 3-column stats grid with a 4-column grid**

Find the block starting with `<div className="grid grid-cols-3 gap-4">` (line 26) and replace the entire block with:

```tsx
<div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
  <Card>
    <CardHeader className="pb-2">
      <CardTitle className="text-sm text-zinc-400 font-normal">Total SCU Used</CardTitle>
    </CardHeader>
    <CardContent>
      <p className="text-3xl font-bold">{totalSCU.toLocaleString()}</p>
    </CardContent>
  </Card>
  <Card>
    <CardHeader className="pb-2">
      <CardTitle className="text-sm text-zinc-400 font-normal">Total Cost</CardTitle>
    </CardHeader>
    <CardContent>
      <p className="text-3xl font-bold">${totalUSD.toFixed(2)}</p>
    </CardContent>
  </Card>
  <Card>
    <CardHeader className="pb-2">
      <CardTitle className="text-sm text-zinc-400 font-normal">AI Adoption Rate</CardTitle>
    </CardHeader>
    <CardContent>
      <p className="text-3xl font-bold">
        {adoptionData ? `${(adoptionData.adoption_rate * 100).toFixed(0)}%` : "—"}
      </p>
      <p className="text-xs text-zinc-500 mt-1">
        {adoptionData?.active_users ?? 0} / {adoptionData?.total_users ?? 0} employees
      </p>
    </CardContent>
  </Card>
  <Card>
    <CardHeader className="pb-2">
      <CardTitle className="text-sm text-zinc-400 font-normal">Month-end Forecast</CardTitle>
    </CardHeader>
    <CardContent>
      <p className="text-3xl font-bold">
        {forecast
          ? forecast.projected_scu.toLocaleString(undefined, { maximumFractionDigits: 0 })
          : "—"}
        <span className="text-sm font-normal text-zinc-500 ml-1">SCU</span>
      </p>
      {forecast && (
        <p className={`text-xs mt-1 ${forecast.trend_pct >= 0 ? "text-red-400" : "text-emerald-400"}`}>
          {forecast.trend_pct >= 0 ? "↑" : "↓"} {Math.abs(forecast.trend_pct).toFixed(1)}% vs last month
          <span className="text-zinc-500 ml-1">
            ({forecast.days_elapsed}/{forecast.days_in_month} days)
          </span>
        </p>
      )}
    </CardContent>
  </Card>
</div>
```

- [ ] **Step 7: Add the inactive users card after the closing `</Card>` of the Usage Details card**

```tsx
{inactiveUsers.length > 0 && (
  <Card>
    <CardHeader>
      <CardTitle className="flex items-center gap-2">
        Low Activity Employees
        <span className="text-sm font-normal text-zinc-400">
          (&lt;3 active days this month)
        </span>
      </CardTitle>
    </CardHeader>
    <CardContent className="p-0">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-zinc-800 text-zinc-400">
            <th className="text-left py-2 px-4 font-medium">Employee</th>
            <th className="text-left py-2 font-medium">Department</th>
            <th className="text-left py-2 font-medium">Role</th>
            <th className="text-right py-2 font-medium">Active Days</th>
            <th className="text-right py-2 px-4 font-medium">Last Active</th>
          </tr>
        </thead>
        <tbody>
          {inactiveUsers.map((u) => (
            <tr key={u.user_id} className="border-b border-zinc-800/50">
              <td className="py-2 px-4">
                <p>{u.name}</p>
                <p className="text-zinc-500 text-xs">{u.email}</p>
              </td>
              <td className="py-2 text-zinc-400">{u.department || "—"}</td>
              <td className="py-2 text-zinc-400">{u.job_role || "—"}</td>
              <td className="py-2 text-right">
                <span className={u.active_days === 0 ? "text-red-400" : "text-yellow-400"}>
                  {u.active_days}
                </span>
              </td>
              <td className="py-2 px-4 text-right text-zinc-500">
                {u.last_active_at ?? "Never"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </CardContent>
  </Card>
)}
```

- [ ] **Step 8: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 9: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/App.tsx dashboard/src/components/Layout.tsx dashboard/src/pages/admin/DashboardPage.tsx
git commit -m "feat(dashboard): add forecast card, inactive users, department report nav"
```

---

## Self-Review

### 1. Spec Coverage

| Requirement | Covered By |
|---|---|
| 部门成本分摊报告 (department cost breakdown table) | Task 1 `GetDepartmentSummary`, Task 2 `getDepartmentSummary` handler, Task 4 `DepartmentReportPage` |
| CSV 导出 (one-click CSV download) | Task 2 `exportDepartmentCSV` handler, Task 3 `exportDepartmentCSV` client fn, Task 4 Export CSV button |
| 预算预测 (linear month-end projection) | Task 1 `GetBudgetForecast`, Task 2 `getBudgetForecast` handler, Task 5 forecast card |
| Prior-month comparison + trend % | Task 1 `TrendPct` field, Task 5 trend arrow display (↑/↓ with colour coding) |
| Days elapsed out of days in month | Task 1 `DaysElapsed`/`DaysInMonth` fields, Task 5 `(days_elapsed/days_in_month days)` label |
| 未使用 AI 员工识别 (inactive user list) | Task 1 `GetInactiveUsers`, Task 2 `getInactiveUsers` handler, Task 5 Low Activity Employees card |
| Default threshold 3 active days | Task 2 `maxDays := 3` default, Task 3 `maxDays = 3` default arg, Task 5 `getInactiveUsers(currentMonth, 3)` |
| Admin-only enforcement (backend) | Task 2: `claims.Role != "admin"` check in every handler |
| Admin-only enforcement (frontend) | Task 4/5: `<ProtectedRoute adminOnly>` wrapping the new route |
| Sidebar nav entry | Task 5b: `{ label: "Reports", href: "/admin/reports" }` in `adminNavItems` |
| Route `/admin/reports` | Task 5a: `<Route path="admin/reports">` |

All spec requirements are covered.

### 2. Placeholder Scan

No TBD, TODO, vague instructions, or stub code is present. Every step provides complete, compilable code.

### 3. Type Consistency

- `DeptSummary` Go struct fields → TS interface fields: `Department`/`department`, `UserCount`/`user_count`, `ActiveUsers`/`active_users`, `TotalSCU`/`total_scu`, `TotalUSD`/`total_usd`, `RequestCount`/`request_count` — all consistent via `json` struct tags.
- `BudgetForecast` Go → TS: all nine fields match snake_case via json tags.
- `InactiveUser.LastActiveAt *string` (Go) → `last_active_at: string | null` (TS) — null safety preserved correctly.
- `getDepartmentSummary` defined in Task 3, imported in Task 4 as `import { getDepartmentSummary, exportDepartmentCSV } from "../../api/client"` — consistent.
- `getBudgetForecast` / `getInactiveUsers` defined in Task 3, imported in Task 5 — consistent.
- `RegisterUsageAdminRoutes(app fiber.Router, svc *services.UsageService)` defined in Task 2, called in Task 2 Step 3 as `api.RegisterUsageAdminRoutes(protected, usageSvc)` where `usageSvc` is `*services.UsageService` — consistent.
- `UsageServiceInterface` expanded in Task 1 Step 2 to include all 5 methods. The existing test mock in `admin/api/usage_test.go` satisfies only the original 2-method interface used by `RegisterUsageRoutes` — those tests are unaffected because `RegisterUsageAdminRoutes` takes the concrete type, not the interface.
- `forecast.projected_scu`, `forecast.trend_pct`, `forecast.days_elapsed`, `forecast.days_in_month` in Task 5 JSX all match `BudgetForecast` TS interface fields defined in Task 3.
