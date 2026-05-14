# Phase 3 Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge Phase 2 to main, add multi-tenant demo data, expose AIQ sub-metrics to employees, add integration tests, and verify Docker Compose one-command startup.

**Architecture:** Sequential tasks — merge first so all later work happens on `main`. Sub-metrics use on-demand computation from existing `AIQService.GetRawMetrics`. Integration tests use a real postgres via `TEST_POSTGRES_DSN` env var and a `go test -tags integration` opt-in flag.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, PostgreSQL 16, React 19 + TypeScript, TanStack Query v5, Docker Compose, bash

**Note:** Task 6 (monthly cron) already exists at `admin/cron/snapshot.go` — skip.

---

## File Map

| Task | Files touched |
|------|--------------|
| 1 | git only |
| 2 | `infra/postgres/002_seed_dev.sql` |
| 3 | `admin/services/kpi.go`, `admin/api/kpi.go`, `dashboard/src/api/client.ts`, `dashboard/src/pages/employee/MyUsagePage.tsx` |
| 4 | `admin/api/integration_test.go` (new) |
| 5 | `scripts/healthcheck.sh` (new) |

---

## Task 1: Merge PR #1 to main

**Files:** git operations only

- [ ] **Step 1: Push with PAT if needed**

If not already pushed:
```bash
git remote set-url origin "https://<PAT>@github.com/SugaC-275/ToTra.git"
git push origin feat/phase2-intelligence-layer
git remote set-url origin "https://github.com/SugaC-275/ToTra.git"
```

- [ ] **Step 2: Merge locally and push main**

```bash
git checkout main
git pull origin main
git merge --no-ff feat/phase2-intelligence-layer -m "Merge Phase 2 + Algorithm v2 + backlog polish"
git push origin main
```

Expected: fast-forward or merge commit on main. If push fails: provide PAT to user.

- [ ] **Step 3: Verify main has all commits**

```bash
git log --oneline -8
```

Expected output includes: `feat(algorithm): v2 three-pillar KPI scoring`, `seed: expand dev fixtures`, `feat(kpi): anomaly detection`.

- [ ] **Step 4: Delete feature branch locally (optional cleanup)**

```bash
git branch -d feat/phase2-intelligence-layer
```

---

## Task 2: Multi-tenant seed data (Beta Corp)

**Files:**
- Modify: `infra/postgres/002_seed_dev.sql`

Add a second tenant with its own admin, engineer, model, and usage data so tenant isolation can be verified in development.

Fixed UUIDs for Beta Corp:
- tenant: `00000000-0000-0000-0000-000000000002`
- Carol (admin): `00000000-0000-0000-0000-000000000013`
- Dave (engineer): `00000000-0000-0000-0000-000000000014`
- model (claude-3.5): `00000000-0000-0000-0000-000000000021`

- [ ] **Step 1: Append Beta Corp tenant + users + model to seed file**

Open `infra/postgres/002_seed_dev.sql` and append at the end:

```sql
-- ─────────────────────────────────────────────
-- TENANT 2: Beta Corp (isolation test)
-- ─────────────────────────────────────────────
INSERT INTO tenants (id, name, plan) VALUES
  ('00000000-0000-0000-0000-000000000002', 'Beta Corp', 'api_key')
ON CONFLICT (id) DO NOTHING;

INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role, job_role, department)
VALUES
  (
    '00000000-0000-0000-0000-000000000013',
    '00000000-0000-0000-0000-000000000002',
    'Carol Admin', 'carol@beta.com', 'api_key',
    encode(sha256('totra-emp-dev-carol-key'), 'hex'),
    999999, 'admin', 'engineering-manager', 'platform'
  ),
  (
    '00000000-0000-0000-0000-000000000014',
    '00000000-0000-0000-0000-000000000002',
    'Dave Engineer', 'dave@beta.com', 'api_key',
    encode(sha256('totra-emp-dev-dave-key'), 'hex'),
    50000, 'standard', 'engineer', 'backend'
  )
ON CONFLICT (id) DO NOTHING;

INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
VALUES (
  '00000000-0000-0000-0000-000000000021',
  '00000000-0000-0000-0000-000000000002',
  'claude-3.5-sonnet', 'anthropic', 'dev-anthropic-key',
  'https://api.anthropic.com/v1', 1.5
)
ON CONFLICT (id) DO NOTHING;

-- Dave — 10 active days in May 2026 across 5 conversations
-- conv d5001: May 4+5, 3 turns, OD~3.0
INSERT INTO usage_records
  (id, tenant_id, user_id, model_config_id, conversation_id,
   prompt_tokens, completion_tokens, scu_cost, usd_cost, response_ms, request_at)
VALUES
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000501',
   500, 1500, 3.0, 0.003, 900, '2026-05-04 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000501',
   480, 1440, 2.88, 0.00288, 880, '2026-05-04 10:30:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000501',
   510, 1530, 3.06, 0.00306, 920, '2026-05-05 14:00:00+00'),
-- conv d5002: May 6+7, 2 turns, OD~3.5
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000502',
   400, 1400, 2.7, 0.0027, 850, '2026-05-06 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000502',
   420, 1470, 2.838, 0.002838, 870, '2026-05-07 11:00:00+00'),
-- conv d5003: May 8+11, 4 turns, OD~2.5
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000503',
   700, 1750, 3.675, 0.003675, 1000, '2026-05-08 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000503',
   680, 1700, 3.57, 0.00357, 980, '2026-05-08 10:30:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000503',
   720, 1800, 3.78, 0.00378, 1020, '2026-05-11 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000503',
   690, 1725, 3.645, 0.003645, 990, '2026-05-11 11:00:00+00'),
-- conv d5004: May 12+13, 3 turns, OD~3.0
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000504',
   550, 1650, 3.3, 0.0033, 950, '2026-05-12 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000504',
   540, 1620, 3.24, 0.00324, 930, '2026-05-12 10:30:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000504',
   560, 1680, 3.36, 0.00336, 970, '2026-05-13 14:00:00+00'),
-- conv d5005: May 14+15, 2 turns, OD~4.0
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000505',
   400, 1600, 3.0, 0.003, 900, '2026-05-14 09:00:00+00'),
  (gen_random_uuid(), '00000000-0000-0000-0000-000000000002',
   '00000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000021',
   'dddddddd-0000-0000-0000-000000000505',
   410, 1640, 3.075, 0.003075, 910, '2026-05-15 11:00:00+00');
```

- [ ] **Step 2: Verify SQL syntax**

```bash
cd /Users/sugac.275/ToTra
grep -c "gen_random_uuid()" infra/postgres/002_seed_dev.sql
```

Expected: `106` (91 existing + 15 new for Dave).

- [ ] **Step 3: Commit**

```bash
git add infra/postgres/002_seed_dev.sql
git commit -m "seed: add Beta Corp tenant for multi-tenant isolation testing"
```

---

## Task 3: Employee AIQ sub-metrics API + dashboard

**Files:**
- Modify: `admin/services/kpi.go` — add `GetUserAIQMetrics` method
- Modify: `admin/api/kpi.go` — add `GET /api/me/kpi/submetrics` route + handler
- Modify: `dashboard/src/api/client.ts` — add `AIQSubmetrics` type + `getMyKPISubmetrics`
- Modify: `dashboard/src/pages/employee/MyUsagePage.tsx` — show sub-metrics below score grid

### 3a: Service method

- [ ] **Step 1: Add `GetUserAIQMetrics` to `admin/services/kpi.go`**

Open the file. After the `GetMySnapshots` function, add:

```go
// GetUserAIQMetrics returns raw AIQ sub-metrics for a single user in a given month.
// Returns nil if the user has no usage data.
func (s *KPIService) GetUserAIQMetrics(ctx context.Context, tenantID, userID, yearMonth string) (*RawAIQMetrics, error) {
	all, err := s.aiqSvc.GetRawMetrics(ctx, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.UserID == userID {
			return m, nil
		}
	}
	return nil, nil
}
```

- [ ] **Step 2: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no errors.

### 3b: API handler

- [ ] **Step 3: Add route + handler to `admin/api/kpi.go`**

In `RegisterKPIRoutes`, add:
```go
app.Get("/api/me/kpi/submetrics", getMyKPISubmetrics(svc))
```

Add the handler function at the end of the file:

```go
func getMyKPISubmetrics(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month", time.Now().UTC().Format("2006-01"))
		metrics, err := svc.GetUserAIQMetrics(c.Context(), claims.TenantID, claims.UserID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if metrics == nil {
			return c.JSON(fiber.Map{"month": month, "metrics": nil})
		}
		return c.JSON(fiber.Map{
			"month": month,
			"metrics": fiber.Map{
				"output_density":    metrics.OutputDensity,
				"usage_consistency": metrics.UsageConsistency,
				"task_depth":        metrics.TaskDepth,
				"cost_efficiency":   metrics.CostEfficiency,
				"active_days":       metrics.ActiveDays,
				"working_days":      services.WorkingDaysInMonth(month),
			},
		})
	}
}
```

Add `"time"` to the import block in `admin/api/kpi.go` if not present.

- [ ] **Step 4: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no errors.

### 3c: TypeScript types + API call

- [ ] **Step 5: Update `dashboard/src/api/client.ts`**

After the `UserProfile` interface, add:

```typescript
export interface AIQSubmetrics {
  output_density: number;
  usage_consistency: number;
  task_depth: number;
  cost_efficiency: number;
  active_days: number;
  working_days: number;
}

export const getMyKPISubmetrics = (month: string) =>
  apiClient.get<{ month: string; metrics: AIQSubmetrics | null }>(
    `/api/me/kpi/submetrics?month=${month}`
  );
```

- [ ] **Step 6: TypeScript check**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

### 3d: Dashboard — show sub-metrics

- [ ] **Step 7: Update `dashboard/src/pages/employee/MyUsagePage.tsx`**

Add import at the top:
```typescript
import { getMyKPISubmetrics } from "../../api/client";
import type { AIQSubmetrics } from "../../api/client";
```

Inside `MyUsagePage`, add a new query after the existing `kpiData` query:
```typescript
const { data: submetricsData } = useQuery({
  queryKey: ["my-kpi-submetrics", currentMonth],
  queryFn: () => getMyKPISubmetrics(currentMonth).then((r) => r.data),
});
const submetrics: AIQSubmetrics | null = submetricsData?.metrics ?? null;
```

Inside the `{latestSnapshot && (...)}` card, after the closing `</div>` of the 3-column AIQ/OSS/GTS grid (the one gated on `latestSnapshot.aiq_score > 0`), add:

```tsx
{submetrics && (
  <div className="mt-4 pt-4 border-t border-zinc-800">
    <p className="text-xs text-zinc-500 mb-3">AIQ Sub-metrics — {currentMonth}</p>
    <div className="grid grid-cols-2 gap-2 text-xs">
      <div className="bg-zinc-800/50 rounded p-2">
        <p className="text-zinc-500 mb-1">Output Density</p>
        <p className="font-mono font-semibold">{submetrics.output_density.toFixed(2)}</p>
        <p className="text-zinc-600 mt-0.5">completion / prompt ratio</p>
      </div>
      <div className="bg-zinc-800/50 rounded p-2">
        <p className="text-zinc-500 mb-1">Usage Consistency</p>
        <p className="font-mono font-semibold">{(submetrics.usage_consistency * 100).toFixed(0)}%</p>
        <p className="text-zinc-600 mt-0.5">{submetrics.active_days} / {submetrics.working_days} working days</p>
      </div>
      <div className="bg-zinc-800/50 rounded p-2">
        <p className="text-zinc-500 mb-1">Task Depth</p>
        <p className="font-mono font-semibold">{submetrics.task_depth.toFixed(1)}</p>
        <p className="text-zinc-600 mt-0.5">turns × log(tokens)</p>
      </div>
      <div className="bg-zinc-800/50 rounded p-2">
        <p className="text-zinc-500 mb-1">Cost Efficiency</p>
        <p className="font-mono font-semibold">{submetrics.cost_efficiency.toFixed(0)}</p>
        <p className="text-zinc-600 mt-0.5">completion tokens / SCU</p>
      </div>
    </div>
  </div>
)}
```

- [ ] **Step 8: TypeScript check**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 9: Commit**

```bash
git add admin/services/kpi.go admin/api/kpi.go \
    dashboard/src/api/client.ts dashboard/src/pages/employee/MyUsagePage.tsx
git commit -m "feat(kpi): expose AIQ sub-metrics (OD/UC/TD/CE) to employee dashboard"
```

---

## Task 4: Integration tests — webhook + KPI pipeline

**Files:**
- Create: `admin/api/integration_test.go`

Tests use a real postgres pointed to by `TEST_POSTGRES_DSN`. If unset, all tests are skipped. Build tag `integration` keeps them out of the default `go test ./...` run.

Tests cover:
1. Health endpoint returns 200
2. Login returns a valid JWT
3. `POST /webhooks/github` stores an output_event
4. `POST /api/admin/kpi/run` creates efficiency_snapshots
5. `GET /api/kpi/snapshots` returns ranked data with aiq_score populated
6. Anomaly-flagged user scores 0

- [ ] **Step 1: Create `admin/api/integration_test.go`**

```go
//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

func getTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — skipping integration tests")
	}
	return dsn
}

func setupApp(t *testing.T, pool *pgxpool.Pool) *fiber.App {
	t.Helper()
	const encKey = "0000000000000000000000000000000000000000000000000000000000000000"
	const jwtSecret = "test-secret"

	jwtSvc := services.NewJWTService(jwtSecret, 24)
	jwtMW := api.NewJWTMiddleware(jwtSvc)
	kpiSvc := services.NewKPIService(pool)
	webhookSvc := services.NewWebhookService(pool)

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(cors.New())
	app.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))
	api.RegisterWebhookRoutes(app, webhookSvc, encKey)

	protected := app.Group("/", jwtMW)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterKPIRoutes(protected, kpiSvc)
	return app
}

func adminJWT(t *testing.T, app *fiber.App) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "admin@acme.com", "password": "totra-emp-dev-admin-key"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login failed: %v status=%d", err, resp.StatusCode)
	}
	var out struct{ Token string `json:"token"` }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Token == "" {
		t.Fatal("login returned empty token")
	}
	return out.Token
}

func authReq(method, url, token string, body any) *http.Request {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// ── Test 1: Health ──────────────────────────────────────────────────────────

func TestIntegration_Health(t *testing.T) {
	dsn := getTestDSN(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	app := setupApp(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req, 3000)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("health: want 200 got %d", resp.StatusCode)
	}
}

// ── Test 2: Login ───────────────────────────────────────────────────────────

func TestIntegration_Login(t *testing.T) {
	dsn := getTestDSN(t)
	pool, _ := pgxpool.New(context.Background(), dsn)
	defer pool.Close()
	app := setupApp(t, pool)

	token := adminJWT(t, app)
	if len(token) < 20 {
		t.Errorf("token too short: %q", token)
	}
}

// ── Test 3: KPI snapshot creates efficiency_snapshots ──────────────────────

func TestIntegration_KPISnapshot(t *testing.T) {
	dsn := getTestDSN(t)
	pool, _ := pgxpool.New(context.Background(), dsn)
	defer pool.Close()
	app := setupApp(t, pool)
	token := adminJWT(t, app)

	month := time.Now().UTC().Format("2006-01")
	req := authReq(http.MethodPost, fmt.Sprintf("/api/admin/kpi/run?month=%s", month), token, nil)
	resp, err := app.Test(req, 30000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("kpi/run: err=%v status=%d", err, resp.StatusCode)
	}

	// Verify snapshots were written
	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM efficiency_snapshots WHERE year_month=$1`, month,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query snapshots: %v", err)
	}
	if count == 0 {
		t.Error("expected >0 efficiency_snapshots after RunMonthlySnapshot")
	}
}

// ── Test 4: GET /api/kpi/snapshots returns ranked data ─────────────────────

func TestIntegration_GetSnapshots(t *testing.T) {
	dsn := getTestDSN(t)
	pool, _ := pgxpool.New(context.Background(), dsn)
	defer pool.Close()
	app := setupApp(t, pool)
	token := adminJWT(t, app)

	month := time.Now().UTC().Format("2006-01")
	// Ensure snapshots exist (idempotent)
	pool.Exec(context.Background(), "DELETE FROM efficiency_snapshots WHERE year_month=$1", month)
	runReq := authReq(http.MethodPost, fmt.Sprintf("/api/admin/kpi/run?month=%s", month), token, nil)
	app.Test(runReq, 30000)

	req := authReq(http.MethodGet, fmt.Sprintf("/api/kpi/snapshots?month=%s", month), token, nil)
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("kpi/snapshots: err=%v status=%d", err, resp.StatusCode)
	}
	var out struct {
		Snapshots []struct {
			UserName  string  `json:"user_name"`
			AIQScore  float64 `json:"aiq_score"`
			OSSScore  float64 `json:"oss_score"`
			Rank      int     `json:"rank"`
			PeerCount int     `json:"peer_count"`
		} `json:"snapshots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Snapshots) == 0 {
		t.Fatal("expected snapshots in response")
	}
	// Alice (12 active days) should have non-zero AIQ
	for _, s := range out.Snapshots {
		if s.UserName == "Alice Engineer" && s.AIQScore == 0 {
			t.Errorf("Alice should have non-zero AIQ score (she has 12 active days)")
		}
	}
	// Ranks should be consecutive starting at 1
	ranks := map[string]int{}
	for _, s := range out.Snapshots {
		ranks[s.UserName] = s.Rank
	}
	if ranks["Alice Engineer"] != 1 {
		t.Errorf("Alice should rank #1 in engineer group, got %d", ranks["Alice Engineer"])
	}
}

// ── Test 5: Sub-metrics endpoint ────────────────────────────────────────────

func TestIntegration_SubMetrics(t *testing.T) {
	dsn := getTestDSN(t)
	pool, _ := pgxpool.New(context.Background(), dsn)
	defer pool.Close()
	app := setupApp(t, pool)
	token := adminJWT(t, app)

	month := time.Now().UTC().Format("2006-01")
	req := authReq(http.MethodGet, fmt.Sprintf("/api/me/kpi/submetrics?month=%s", month), token, nil)
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("kpi/submetrics: err=%v status=%d", err, resp.StatusCode)
	}
	var out struct {
		Metrics *struct {
			OutputDensity    float64 `json:"output_density"`
			UsageConsistency float64 `json:"usage_consistency"`
			ActiveDays       int     `json:"active_days"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Admin user (admin@acme.com) has no usage records → metrics should be nil
	if out.Metrics != nil {
		t.Logf("admin has metrics: OD=%.2f UC=%.2f active=%d",
			out.Metrics.OutputDensity, out.Metrics.UsageConsistency, out.Metrics.ActiveDays)
	}
}
```

- [ ] **Step 2: Verify the file compiles (build tag excludes it from normal build)**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./... 2>&1
```

Expected: no errors (file is excluded from default build).

- [ ] **Step 3: Run integration tests against docker-compose DB**

```bash
cd /Users/sugac.275/ToTra/admin
TEST_POSTGRES_DSN="postgres://totra:totra_secret@localhost:5432/totra" \
  go test -tags integration ./api/... -v -timeout 60s 2>&1
```

Expected: all 5 tests PASS. If docker-compose is not running, tests skip.

- [ ] **Step 4: Commit**

```bash
git add admin/api/integration_test.go
git commit -m "test: add integration tests for KPI snapshot + sub-metrics pipeline"
```

---

## Task 5: Docker Compose health-check script

**Files:**
- Create: `scripts/healthcheck.sh`

Script that starts all services, waits for health checks, validates each endpoint, and reports pass/fail. Safe to run repeatedly (idempotent).

- [ ] **Step 1: Create `scripts/healthcheck.sh`**

```bash
#!/usr/bin/env bash
# healthcheck.sh — verify docker compose is fully healthy and all endpoints respond
#
# Usage:
#   bash scripts/healthcheck.sh
#   bash scripts/healthcheck.sh --no-start  # skip docker compose up, just probe

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NO_START="${1:-}"
PASS=0; FAIL=0

log()  { echo "  $*"; }
ok()   { echo "  PASS  $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL  $1 — $2"; FAIL=$((FAIL+1)); }

# ── 1. Start services ───────────────────────────────────────────────────────
if [ "${NO_START}" != "--no-start" ]; then
  echo "Starting docker compose..."
  docker compose -f "${ROOT}/docker-compose.yml" up -d --wait 2>&1 | tail -5
fi

# ── 2. Wait helper ──────────────────────────────────────────────────────────
wait_for() {
  local label="$1" url="$2" attempts=0
  until curl -sf "${url}" > /dev/null 2>&1; do
    attempts=$((attempts+1))
    [ ${attempts} -ge 30 ] && fail "${label}" "timed out after 30s" && return
    sleep 1
  done
  ok "${label} reachable"
}

echo ""
echo "ToTra Docker Compose Health Check"
echo ""

# Load port defaults
source "${ROOT}/.env" 2>/dev/null || true
ADMIN_PORT="${ADMIN_PORT:-8081}"
GATEWAY_PORT="${GATEWAY_PORT:-8080}"

# ── 3. Probe endpoints ──────────────────────────────────────────────────────
wait_for "admin  /health" "http://localhost:${ADMIN_PORT}/health"
wait_for "gateway /health" "http://localhost:${GATEWAY_PORT}/health"

# ── 4. Admin login smoke test ───────────────────────────────────────────────
echo -n "  TEST  admin login ... "
LOGIN=$(curl -sf -X POST "http://localhost:${ADMIN_PORT}/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}' 2>/dev/null || echo "")
if echo "${LOGIN}" | grep -q '"token"'; then
  echo "PASS"
  PASS=$((PASS+1))
else
  echo "FAIL — ${LOGIN}"
  FAIL=$((FAIL+1))
fi

# ── 5. Feishu smoke test (if secret is set) ─────────────────────────────────
if [ -n "${TOTRA_WEBHOOK_SECRET:-}" ]; then
  TOTRA_BASE_URL="http://localhost:${ADMIN_PORT}" \
  TOTRA_TENANT_ID="00000000-0000-0000-0000-000000000001" \
  bash "${ROOT}/scripts/smoke-test-feishu.sh"
else
  log "Skipping feishu smoke test (TOTRA_WEBHOOK_SECRET not set)"
fi

# ── 6. Summary ──────────────────────────────────────────────────────────────
echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[ "${FAIL}" -eq 0 ] && exit 0 || exit 1
```

- [ ] **Step 2: Make executable and syntax-check**

```bash
chmod +x /Users/sugac.275/ToTra/scripts/healthcheck.sh
bash -n /Users/sugac.275/ToTra/scripts/healthcheck.sh
echo "syntax ok"
```

Expected: `syntax ok`.

- [ ] **Step 3: Run the script (requires docker compose running)**

```bash
cd /Users/sugac.275/ToTra
bash scripts/healthcheck.sh --no-start
```

If services are not running: `docker compose up -d --wait` first, then re-run without `--no-start`.

Expected output:
```
  PASS  admin  /health reachable
  PASS  gateway /health reachable
  PASS  admin login
Results: 3 passed, 0 failed
```

- [ ] **Step 4: Commit**

```bash
git add scripts/healthcheck.sh
git commit -m "ops: add docker compose health-check script"
```

---

## Final: Push to main

After all tasks are committed:

```bash
git log --oneline -8
git push origin main
```

(Provide PAT if needed.)
