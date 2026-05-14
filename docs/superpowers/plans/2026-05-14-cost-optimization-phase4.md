# Cost Optimization Platform Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Budget management (per-tenant monthly USD cap configurable via admin) + off-hours usage analysis (detect and quantify AI spend outside business hours as potential waste). No gateway changes — all logic in admin service + dashboard.

**Architecture:** New `tenant_cost_settings` table (migration 020). Admin gets three services: `CostSettingsService` (CRUD), `BudgetStatusService` (current month spend vs cap), `OffHoursService` (hourly usage breakdown). Three API endpoint groups wired into protected router. `CostCenterPage.tsx` extended with Budget Settings panel and Off-Hours Waste panel.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5 (admin) · React 19 + TanStack Query v5 + Tailwind v4 (dashboard)

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `infra/postgres/020_cost_settings.sql` | `tenant_cost_settings` table |
| Modify | `docker-compose.yml` | Mount migration 020 |
| Create | `admin/services/cost_settings.go` | `CostSettingsService` — Get/Upsert |
| Create | `admin/services/cost_settings_test.go` | Unit tests for validation helpers |
| Create | `admin/services/budget_status.go` | `BudgetStatusService` — current spend vs cap |
| Create | `admin/services/budget_status_test.go` | Unit tests for `BudgetPercentage` |
| Create | `admin/services/off_hours.go` | `OffHoursService` — hourly spend breakdown |
| Create | `admin/services/off_hours_test.go` | Unit tests for `IsWorkHour` |
| Create | `admin/api/cost_settings.go` | 5 endpoints: cost settings + budget status + off-hours |
| Create | `admin/api/cost_settings_test.go` | Handler tests |
| Modify | `admin/main.go` | Wire new services + routes |
| Modify | `dashboard/src/pages/admin/CostCenterPage.tsx` | Budget settings form + budget status card + off-hours panel |

---

### Task 1: DB Migration + docker-compose

**Files:**
- Create: `infra/postgres/020_cost_settings.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Create migration**

```sql
-- infra/postgres/020_cost_settings.sql
CREATE TABLE IF NOT EXISTS tenant_cost_settings (
    id                 BIGSERIAL   PRIMARY KEY,
    tenant_id          TEXT        NOT NULL UNIQUE,
    monthly_budget_usd NUMERIC(12,2),
    work_start_hour    INT         NOT NULL DEFAULT 9,
    work_end_hour      INT         NOT NULL DEFAULT 18,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cost_settings_tenant ON tenant_cost_settings(tenant_id);
```

- [ ] **Step 2: Mount in docker-compose.yml**

In the `postgres` service volumes block, after the line mounting `019_policy_rules.sql`, add:

```yaml
      - ./infra/postgres/020_cost_settings.sql:/docker-entrypoint-initdb.d/020_cost_settings.sql:ro
```

If `019_policy_rules.sql` is not yet present (depends on branch), add after `018_routing_events.sql` instead.

- [ ] **Step 3: Commit**

```bash
git add infra/postgres/020_cost_settings.sql docker-compose.yml
git commit -m "feat(cost): migration 020 — tenant_cost_settings table"
```

---

### Task 2: Cost Settings, Budget Status, Off-Hours Services (TDD)

**Files:**
- Create: `admin/services/cost_settings.go`
- Create: `admin/services/cost_settings_test.go`
- Create: `admin/services/budget_status.go`
- Create: `admin/services/budget_status_test.go`
- Create: `admin/services/off_hours.go`
- Create: `admin/services/off_hours_test.go`

- [ ] **Step 1: Write failing tests**

```go
// admin/services/cost_settings_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestValidateWorkHours_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateWorkHours(9, 18))
	assert.NoError(t, services.ValidateWorkHours(0, 23))
}

func TestValidateWorkHours_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateWorkHours(18, 9))  // end <= start
	assert.Error(t, services.ValidateWorkHours(-1, 10)) // out of range
	assert.Error(t, services.ValidateWorkHours(0, 25))  // out of range
}
```

```go
// admin/services/budget_status_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestBudgetPercentage_Normal(t *testing.T) {
	assert.InDelta(t, 50.0, services.BudgetPercentage(50, 100), 0.001)
}

func TestBudgetPercentage_Zero(t *testing.T) {
	assert.Equal(t, 0.0, services.BudgetPercentage(0, 100))
}

func TestBudgetPercentage_NoBudget(t *testing.T) {
	assert.Equal(t, 0.0, services.BudgetPercentage(50, 0))
}

func TestBudgetPercentage_Over(t *testing.T) {
	assert.InDelta(t, 120.0, services.BudgetPercentage(120, 100), 0.001)
}
```

```go
// admin/services/off_hours_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestIsWorkHour_During(t *testing.T) {
	assert.True(t, services.IsWorkHour(9, 9, 18))
	assert.True(t, services.IsWorkHour(17, 9, 18))
}

func TestIsWorkHour_Outside(t *testing.T) {
	assert.False(t, services.IsWorkHour(8, 9, 18))
	assert.False(t, services.IsWorkHour(18, 9, 18))
	assert.False(t, services.IsWorkHour(23, 9, 18))
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./services/ -run 'TestValidateWorkHours|TestBudgetPercentage|TestIsWorkHour' -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 3: Implement cost_settings.go**

```go
// admin/services/cost_settings.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidateWorkHours checks that work hours are valid (0-23, start < end).
func ValidateWorkHours(start, end int) error {
	if start < 0 || start > 23 || end < 0 || end > 23 {
		return fmt.Errorf("work hours must be in range 0-23")
	}
	if end <= start {
		return fmt.Errorf("work_end_hour must be greater than work_start_hour")
	}
	return nil
}

type CostSettings struct {
	TenantID          string   `json:"tenant_id"`
	MonthlyBudgetUSD  *float64 `json:"monthly_budget_usd"` // nil = not set
	WorkStartHour     int      `json:"work_start_hour"`
	WorkEndHour       int      `json:"work_end_hour"`
	UpdatedAt         string   `json:"updated_at"`
}

type CostSettingsService struct{ pool *pgxpool.Pool }

func NewCostSettingsService(pool *pgxpool.Pool) *CostSettingsService {
	return &CostSettingsService{pool: pool}
}

func (s *CostSettingsService) Get(ctx context.Context, tenantID string) (*CostSettings, error) {
	cs := &CostSettings{
		TenantID:      tenantID,
		WorkStartHour: 9,
		WorkEndHour:   18,
	}
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT monthly_budget_usd, work_start_hour, work_end_hour, updated_at
		FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&cs.MonthlyBudgetUSD, &cs.WorkStartHour, &cs.WorkEndHour, &updatedAt)
	if err != nil {
		// No row = return defaults
		cs.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return cs, nil
	}
	cs.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return cs, nil
}

func (s *CostSettingsService) Upsert(ctx context.Context, tenantID string, monthlyBudgetUSD *float64, workStartHour, workEndHour int) (*CostSettings, error) {
	if err := ValidateWorkHours(workStartHour, workEndHour); err != nil {
		return nil, err
	}
	cs := &CostSettings{TenantID: tenantID, MonthlyBudgetUSD: monthlyBudgetUSD, WorkStartHour: workStartHour, WorkEndHour: workEndHour}
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_cost_settings(tenant_id, monthly_budget_usd, work_start_hour, work_end_hour)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE
		  SET monthly_budget_usd=$2, work_start_hour=$3, work_end_hour=$4, updated_at=NOW()
		RETURNING monthly_budget_usd, work_start_hour, work_end_hour, updated_at
	`, tenantID, monthlyBudgetUSD, workStartHour, workEndHour).Scan(
		&cs.MonthlyBudgetUSD, &cs.WorkStartHour, &cs.WorkEndHour, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("cost settings upsert: %w", err)
	}
	cs.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return cs, nil
}
```

- [ ] **Step 4: Implement budget_status.go**

```go
// admin/services/budget_status.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BudgetPercentage returns (spent/budget)*100. Returns 0 if budget is 0.
func BudgetPercentage(spent, budget float64) float64 {
	if budget == 0 {
		return 0
	}
	return spent / budget * 100
}

type BudgetStatus struct {
	TenantID         string   `json:"tenant_id"`
	YearMonth        string   `json:"year_month"`
	SpentUSD         float64  `json:"spent_usd"`
	MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
	UsedPercent      float64  `json:"used_percent"`
	IsOverBudget     bool     `json:"is_over_budget"`
	GeneratedAt      string   `json:"generated_at"`
}

type BudgetStatusService struct{ pool *pgxpool.Pool }

func NewBudgetStatusService(pool *pgxpool.Pool) *BudgetStatusService {
	return &BudgetStatusService{pool: pool}
}

func (s *BudgetStatusService) GetBudgetStatus(ctx context.Context, tenantID string) (*BudgetStatus, error) {
	yearMonth := time.Now().UTC().Format("2006-01")

	var spentUSD float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(usd_cost), 0)
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
	`, tenantID, yearMonth).Scan(&spentUSD)
	if err != nil {
		return nil, fmt.Errorf("budget status spend query: %w", err)
	}

	var budgetUSD *float64
	_ = s.pool.QueryRow(ctx, `
		SELECT monthly_budget_usd FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&budgetUSD)

	usedPercent := 0.0
	isOver := false
	if budgetUSD != nil {
		usedPercent = BudgetPercentage(spentUSD, *budgetUSD)
		isOver = spentUSD > *budgetUSD
	}

	return &BudgetStatus{
		TenantID:         tenantID,
		YearMonth:        yearMonth,
		SpentUSD:         spentUSD,
		MonthlyBudgetUSD: budgetUSD,
		UsedPercent:      usedPercent,
		IsOverBudget:     isOver,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

- [ ] **Step 5: Implement off_hours.go**

```go
// admin/services/off_hours.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IsWorkHour returns true if hour is within [workStart, workEnd).
func IsWorkHour(hour, workStart, workEnd int) bool {
	return hour >= workStart && hour < workEnd
}

type HourlySpend struct {
	Hour      int     `json:"hour"`
	RequestCount int64 `json:"request_count"`
	SpentUSD  float64 `json:"spent_usd"`
	IsWorkHour bool   `json:"is_work_hour"`
}

type OffHoursReport struct {
	TenantID        string        `json:"tenant_id"`
	YearMonth       string        `json:"year_month"`
	WorkStartHour   int           `json:"work_start_hour"`
	WorkEndHour     int           `json:"work_end_hour"`
	TotalUSD        float64       `json:"total_usd"`
	OffHoursUSD     float64       `json:"off_hours_usd"`
	OffHoursPercent float64       `json:"off_hours_percent"`
	HourlyBreakdown []HourlySpend `json:"hourly_breakdown"`
	GeneratedAt     string        `json:"generated_at"`
}

type OffHoursService struct{ pool *pgxpool.Pool }

func NewOffHoursService(pool *pgxpool.Pool) *OffHoursService {
	return &OffHoursService{pool: pool}
}

func (s *OffHoursService) GetOffHoursReport(ctx context.Context, tenantID, yearMonth string) (*OffHoursReport, error) {
	if yearMonth == "" {
		yearMonth = time.Now().UTC().Format("2006-01")
	}

	// Load work hours setting (defaults: 9-18)
	workStart, workEnd := 9, 18
	_ = s.pool.QueryRow(ctx, `
		SELECT work_start_hour, work_end_hour FROM tenant_cost_settings WHERE tenant_id = $1
	`, tenantID).Scan(&workStart, &workEnd)

	rows, err := s.pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM request_at AT TIME ZONE 'UTC')::int AS hour,
		       COUNT(*) AS request_count,
		       COALESCE(SUM(usd_cost), 0) AS spent_usd
		FROM usage_records
		WHERE tenant_id = $1
		  AND to_char(request_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		GROUP BY hour
		ORDER BY hour ASC
	`, tenantID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("off hours query: %w", err)
	}
	defer rows.Close()

	hourMap := make(map[int]HourlySpend, 24)
	for rows.Next() {
		var h HourlySpend
		if err := rows.Scan(&h.Hour, &h.RequestCount, &h.SpentUSD); err != nil {
			return nil, fmt.Errorf("off hours scan: %w", err)
		}
		h.IsWorkHour = IsWorkHour(h.Hour, workStart, workEnd)
		hourMap[h.Hour] = h
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("off hours rows: %w", err)
	}

	breakdown := make([]HourlySpend, 24)
	var totalUSD, offHoursUSD float64
	for i := 0; i < 24; i++ {
		h := hourMap[i]
		h.Hour = i
		h.IsWorkHour = IsWorkHour(i, workStart, workEnd)
		breakdown[i] = h
		totalUSD += h.SpentUSD
		if !h.IsWorkHour {
			offHoursUSD += h.SpentUSD
		}
	}

	offPct := 0.0
	if totalUSD > 0 {
		offPct = offHoursUSD / totalUSD * 100
	}

	return &OffHoursReport{
		TenantID:        tenantID,
		YearMonth:       yearMonth,
		WorkStartHour:   workStart,
		WorkEndHour:     workEnd,
		TotalUSD:        totalUSD,
		OffHoursUSD:     offHoursUSD,
		OffHoursPercent: offPct,
		HourlyBreakdown: breakdown,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

- [ ] **Step 6: Run all service tests**

```bash
cd /path/to/worktree/admin && go test ./services/ -run 'TestValidateWorkHours|TestBudgetPercentage|TestIsWorkHour' -v
```

Expected: all 9 tests PASS.

- [ ] **Step 7: Commit**

```bash
git add admin/services/cost_settings.go admin/services/cost_settings_test.go \
        admin/services/budget_status.go admin/services/budget_status_test.go \
        admin/services/off_hours.go admin/services/off_hours_test.go
git commit -m "feat(cost): CostSettingsService + BudgetStatusService + OffHoursService"
```

---

### Task 3: Cost Settings API Endpoints + Wire Main

**Files:**
- Create: `admin/api/cost_settings.go`
- Create: `admin/api/cost_settings_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing handler tests**

```go
// admin/api/cost_settings_test.go
package api_test

import (
	"bytes"
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

type stubCostSettingsSvc struct{}

func (s *stubCostSettingsSvc) Get(_ context.Context, tenantID string) (*services.CostSettings, error) {
	budget := 500.0
	return &services.CostSettings{TenantID: tenantID, MonthlyBudgetUSD: &budget, WorkStartHour: 9, WorkEndHour: 18}, nil
}
func (s *stubCostSettingsSvc) Upsert(_ context.Context, tenantID string, budget *float64, start, end int) (*services.CostSettings, error) {
	return &services.CostSettings{TenantID: tenantID, MonthlyBudgetUSD: budget, WorkStartHour: start, WorkEndHour: end}, nil
}

type stubBudgetStatusSvc struct{}

func (s *stubBudgetStatusSvc) GetBudgetStatus(_ context.Context, tenantID string) (*services.BudgetStatus, error) {
	budget := 500.0
	return &services.BudgetStatus{TenantID: tenantID, SpentUSD: 123.45, MonthlyBudgetUSD: &budget, UsedPercent: 24.69}, nil
}

type stubOffHoursSvc struct{}

func (s *stubOffHoursSvc) GetOffHoursReport(_ context.Context, tenantID, _ string) (*services.OffHoursReport, error) {
	return &services.OffHoursReport{TenantID: tenantID, OffHoursPercent: 12.5}, nil
}

func makeCostSettingsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostSettingsRoutes(app, &stubCostSettingsSvc{}, &stubBudgetStatusSvc{}, &stubOffHoursSvc{})
	return app
}

func TestCostSettings_Get(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/settings", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCostSettings_Upsert(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"monthly_budget_usd": 1000.0, "work_start_hour": 8, "work_end_hour": 17})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/cost/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := makeCostSettingsApp().Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestBudgetStatus(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/budget-status", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var body services.BudgetStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.InDelta(t, 123.45, body.SpentUSD, 0.01)
}

func TestOffHoursReport(t *testing.T) {
	resp, err := makeCostSettingsApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/off-hours", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCostSettings_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterCostSettingsRoutes(app, &stubCostSettingsSvc{}, &stubBudgetStatusSvc{}, &stubOffHoursSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/cost/settings", nil))
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./api/ -run TestCostSettings -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 3: Implement API handler**

```go
// admin/api/cost_settings.go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type CostSettingsServiceIface interface {
	Get(ctx context.Context, tenantID string) (*services.CostSettings, error)
	Upsert(ctx context.Context, tenantID string, monthlyBudgetUSD *float64, workStartHour, workEndHour int) (*services.CostSettings, error)
}

type BudgetStatusServiceIface interface {
	GetBudgetStatus(ctx context.Context, tenantID string) (*services.BudgetStatus, error)
}

type OffHoursServiceIface interface {
	GetOffHoursReport(ctx context.Context, tenantID, yearMonth string) (*services.OffHoursReport, error)
}

func RegisterCostSettingsRoutes(r fiber.Router, settingsSvc CostSettingsServiceIface, budgetSvc BudgetStatusServiceIface, offHoursSvc OffHoursServiceIface) {
	r.Get("/api/admin/cost/settings", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := settingsSvc.Get(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Put("/api/admin/cost/settings", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
			WorkStartHour    int      `json:"work_start_hour"`
			WorkEndHour      int      `json:"work_end_hour"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		result, err := settingsSvc.Upsert(c.Context(), claims.TenantID, body.MonthlyBudgetUSD, body.WorkStartHour, body.WorkEndHour)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/budget-status", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		result, err := budgetSvc.GetBudgetStatus(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})

	r.Get("/api/admin/cost/off-hours", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		yearMonth := c.Query("month")
		result, err := offHoursSvc.GetOffHoursReport(c.Context(), claims.TenantID, yearMonth)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	})
}
```

- [ ] **Step 4: Wire into admin/main.go**

Read `admin/main.go`. After `costBenchmarkSvc` wiring (or at the end of cost services block), add:

```go
costSettingsSvc := services.NewCostSettingsService(pool)
budgetStatusSvc := services.NewBudgetStatusService(pool)
offHoursSvc := services.NewOffHoursService(pool)
```

After `api.RegisterCostAdvancedRoutes(protected, ...)`:

```go
api.RegisterCostSettingsRoutes(protected, costSettingsSvc, budgetStatusSvc, offHoursSvc)
```

- [ ] **Step 5: Run all admin tests**

```bash
cd /path/to/worktree/admin && go test ./... -count=1
```

Expected: all PASS.

- [ ] **Step 6: Build**

```bash
cd /path/to/worktree/admin && go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add admin/api/cost_settings.go admin/api/cost_settings_test.go admin/main.go
git commit -m "feat(cost): budget settings + budget status + off-hours report endpoints"
```

---

### Task 4: Dashboard — Budget Settings + Off-Hours Panels

**Files:**
- Modify: `dashboard/src/pages/admin/CostCenterPage.tsx`

Read the current `CostCenterPage.tsx` at its absolute path in the worktree before editing. Follow the exact same import pattern, auth token, and query fn structure as the existing code.

- [ ] **Step 1: TypeScript baseline**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit 2>&1 | head -10
```

Expected: 0 errors.

- [ ] **Step 2: Add budget settings section**

Add three new `useQuery` calls and a `useMutation` for settings update, alongside existing queries:

```tsx
const { data: settings, isLoading: settingsLoading } = useQuery({
  queryKey: ["cost-settings"],
  queryFn: () =>
    fetch("/api/admin/cost/settings", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});

const { data: budgetStatus } = useQuery({
  queryKey: ["cost-budget-status"],
  queryFn: () =>
    fetch("/api/admin/cost/budget-status", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});

const { data: offHours, isLoading: offHoursLoading } = useQuery({
  queryKey: ["cost-off-hours"],
  queryFn: () =>
    fetch("/api/admin/cost/off-hours", {
      headers: { Authorization: `Bearer ${token}` },
    }).then((r) => r.json()),
});
```

Check how the existing code handles the `token` — use whatever pattern is already in the file (auth store, useAuth hook, etc.).

- [ ] **Step 3: Add Budget Status card JSX**

Add after the existing savings panel:

```tsx
{/* Budget Status */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-4">Monthly Budget</h2>
  {!budgetStatus ? (
    <p className="text-gray-400 text-sm">Loading…</p>
  ) : budgetStatus.monthly_budget_usd == null ? (
    <p className="text-gray-400 text-sm">No budget configured. Set one below.</p>
  ) : (
    <div className="space-y-3">
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">Spent this month</span>
        <span className="font-semibold">${budgetStatus.spent_usd?.toFixed(2)}</span>
      </div>
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">Monthly budget</span>
        <span className="font-semibold">${budgetStatus.monthly_budget_usd?.toFixed(2)}</span>
      </div>
      {/* Progress bar */}
      <div className="w-full bg-gray-100 rounded-full h-2">
        <div
          className={`h-2 rounded-full ${budgetStatus.is_over_budget ? "bg-red-500" : budgetStatus.used_percent > 80 ? "bg-yellow-400" : "bg-green-500"}`}
          style={{ width: `${Math.min(budgetStatus.used_percent ?? 0, 100)}%` }}
        />
      </div>
      <div className="flex justify-between text-xs text-gray-500">
        <span>{budgetStatus.used_percent?.toFixed(1)}% used</span>
        {budgetStatus.is_over_budget && (
          <span className="text-red-600 font-medium">Over budget!</span>
        )}
      </div>
    </div>
  )}
</div>

{/* Budget Settings */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-4">Budget & Hours Settings</h2>
  {settingsLoading ? (
    <p className="text-gray-400 text-sm">Loading…</p>
  ) : (
    <BudgetSettingsForm settings={settings} token={token} />
  )}
</div>
```

- [ ] **Step 4: Add BudgetSettingsForm component**

Add this component definition before the main `CostCenterPage` export (or as a local function in the same file). Use `useMutation` from TanStack Query v5 and `useQueryClient`:

```tsx
function BudgetSettingsForm({ settings, token }: { settings: any; token: string }) {
  const qc = useQueryClient();
  const [budget, setBudget] = React.useState<string>(settings?.monthly_budget_usd?.toFixed(2) ?? "");
  const [startHour, setStartHour] = React.useState<number>(settings?.work_start_hour ?? 9);
  const [endHour, setEndHour] = React.useState<number>(settings?.work_end_hour ?? 18);

  const mutation = useMutation({
    mutationFn: (body: object) =>
      fetch("/api/admin/cost/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify(body),
      }).then((r) => r.json()),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cost-settings"] });
      qc.invalidateQueries({ queryKey: ["cost-budget-status"] });
      qc.invalidateQueries({ queryKey: ["cost-off-hours"] });
    },
  });

  const hours = Array.from({ length: 24 }, (_, i) => i);

  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm text-gray-600 mb-1">Monthly Budget (USD)</label>
        <input
          type="number"
          min="0"
          step="0.01"
          className="border rounded px-3 py-2 text-sm w-48"
          placeholder="e.g. 1000.00"
          value={budget}
          onChange={(e) => setBudget(e.target.value)}
        />
      </div>
      <div className="flex gap-6">
        <div>
          <label className="block text-sm text-gray-600 mb-1">Work Start Hour (UTC)</label>
          <select className="border rounded px-3 py-2 text-sm" value={startHour} onChange={(e) => setStartHour(+e.target.value)}>
            {hours.map((h) => <option key={h} value={h}>{h}:00</option>)}
          </select>
        </div>
        <div>
          <label className="block text-sm text-gray-600 mb-1">Work End Hour (UTC)</label>
          <select className="border rounded px-3 py-2 text-sm" value={endHour} onChange={(e) => setEndHour(+e.target.value)}>
            {hours.map((h) => <option key={h} value={h}>{h}:00</option>)}
          </select>
        </div>
      </div>
      <button
        className="bg-blue-600 text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        disabled={mutation.isPending}
        onClick={() =>
          mutation.mutate({
            monthly_budget_usd: budget ? parseFloat(budget) : null,
            work_start_hour: startHour,
            work_end_hour: endHour,
          })
        }
      >
        {mutation.isPending ? "Saving…" : "Save Settings"}
      </button>
      {mutation.isSuccess && <p className="text-green-600 text-sm">Saved.</p>}
      {mutation.isError && <p className="text-red-600 text-sm">Failed to save.</p>}
    </div>
  );
}
```

Make sure `React` is imported if using `React.useState`, or switch to named imports (`import { useState } from "react"`). Follow the file's existing import style.

- [ ] **Step 5: Add Off-Hours Waste panel JSX**

Add after the Budget Settings panel:

```tsx
{/* Off-Hours Waste */}
<div className="bg-white rounded-xl shadow p-6">
  <h2 className="text-lg font-semibold mb-1">Off-Hours Usage</h2>
  <p className="text-sm text-gray-500 mb-4">
    AI spend outside configured work hours (UTC). May indicate personal use or automated jobs.
  </p>
  {offHoursLoading ? (
    <p className="text-gray-400">Loading…</p>
  ) : offHours ? (
    <div>
      <div className="grid grid-cols-3 gap-4 mb-4">
        <div className="p-3 bg-gray-50 rounded-lg text-center">
          <div className="text-2xl font-bold text-gray-800">${offHours.total_usd?.toFixed(2)}</div>
          <div className="text-xs text-gray-500 mt-1">Total spend</div>
        </div>
        <div className="p-3 bg-orange-50 rounded-lg text-center">
          <div className="text-2xl font-bold text-orange-700">${offHours.off_hours_usd?.toFixed(2)}</div>
          <div className="text-xs text-gray-500 mt-1">Off-hours spend</div>
        </div>
        <div className="p-3 bg-orange-50 rounded-lg text-center">
          <div className="text-2xl font-bold text-orange-700">{offHours.off_hours_percent?.toFixed(1)}%</div>
          <div className="text-xs text-gray-500 mt-1">Of total</div>
        </div>
      </div>
      <p className="text-xs text-gray-400">
        Work hours: {offHours.work_start_hour}:00–{offHours.work_end_hour}:00 UTC ·{" "}
        {offHours.year_month}
      </p>
    </div>
  ) : (
    <p className="text-gray-400">No data</p>
  )}
</div>
```

- [ ] **Step 6: Add missing imports**

If `useMutation` or `useQueryClient` are not already imported, add them to the TanStack Query import line. If `React` is needed for the form component, add the import.

- [ ] **Step 7: TypeScript check**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit
```

Fix any errors before committing.

- [ ] **Step 8: Commit**

```bash
git add dashboard/src/pages/admin/CostCenterPage.tsx
git commit -m "feat(cost): budget status card + settings form + off-hours waste panel on CostCenterPage"
```

---

## Self-Review

**Spec coverage:**
- ✅ `tenant_cost_settings` table — Task 1
- ✅ `docker-compose.yml` updated — Task 1
- ✅ `ValidateWorkHours` pure fn, tested — Task 2
- ✅ `BudgetPercentage` pure fn, tested — Task 2
- ✅ `IsWorkHour` pure fn, tested — Task 2
- ✅ `CostSettingsService`: Get + Upsert (upsert with conflict) — Task 2
- ✅ `BudgetStatusService`: current month spend vs cap, IsOverBudget — Task 2
- ✅ `OffHoursService`: 24-hour breakdown, off-hours USD + percent — Task 2
- ✅ 4 endpoints (GET/PUT settings, GET budget-status, GET off-hours) — Task 3
- ✅ Admin-only guard on all endpoints — Task 3
- ✅ Budget Status progress bar + over-budget warning — Task 4
- ✅ Budget Settings form (budget cap + work hours) — Task 4
- ✅ Off-Hours waste panel — Task 4

**Placeholder scan:** None.

**Type consistency:**
- `CostSettingsServiceIface.Get/Upsert` signatures match `CostSettingsService` exactly.
- `BudgetStatusServiceIface.GetBudgetStatus` matches `BudgetStatusService`.
- `OffHoursServiceIface.GetOffHoursReport` matches `OffHoursService`.
- Dashboard reads `monthly_budget_usd`, `spent_usd`, `used_percent`, `is_over_budget` from `BudgetStatus` — all present in struct.
- Dashboard reads `total_usd`, `off_hours_usd`, `off_hours_percent`, `work_start_hour`, `work_end_hour`, `year_month` from `OffHoursReport` — all present in struct.
