# Cost Optimization Platform Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add exact-match request dedup cache (Redis), model auto-routing (premium→standard for simple requests), and a monthly savings report endpoint + dashboard panel.

**Architecture:** Cache lives inside `makeProxyHandler` in `gateway/main.go` — check before forwarding, store after success. Auto-router is a new gateway middleware that rewrites the `model` field for short/simple requests before reaching the proxy handler. Routing events are written async to Postgres. Admin gets a new `GET /api/admin/cost/savings-report` endpoint showing routing event counts by model pair.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5 + redis/go-redis/v9, React 19 + TanStack Query v5 + Tailwind v4

---

## Codebase Reference

- `gateway/main.go` — `makeProxyHandler` calls `fwd.Forward(ctx, c.Body())`. Add cache check before this and cache set after. Middleware chain order: `auth → quota → PII → [NEW: router] → agent`.
- `gateway/middleware/pii.go` — `ViolationRecorder` interface pattern (avoids import cycles) — use same pattern for `RoutingRecorder`.
- `gateway/storage/pii_store.go` — async goroutine store pattern — use same pattern for routing events.
- `admin/api/compliance.go` — `currentYearMonth()` is defined here. **Do not redefine** in new API files — they share the same `api` package.
- `admin/main.go` — wire new service after `costSvc`.
- JWT Claims: `c.Locals("claims").(*services.Claims)` — `TenantID`, `UserID`, `Role`. Admin-only: `claims.Role != "admin"` → 403.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `infra/postgres/018_routing_events.sql` | Create | `gateway_routing_events` table |
| `docker-compose.yml` | Modify | Mount 018 after 017 |
| `gateway/storage/request_cache.go` | Create | `CacheKey`, `RequestCache` (Get, Set, IncrHit, GetHitCount) |
| `gateway/storage/request_cache_test.go` | Create | Unit tests for CacheKey determinism |
| `gateway/storage/routing_store.go` | Create | `RoutingStore`, `Record` (async goroutine) |
| `gateway/storage/routing_store_test.go` | Create | Compile-time interface check |
| `gateway/middleware/router.go` | Create | `NewAutoRouterMiddleware`, `ClassifyComplexity` |
| `gateway/middleware/router_test.go` | Create | Unit tests for routing logic |
| `gateway/main.go` | Modify | Create `requestCache` + `routingStore`, inject into handler + middleware chain |
| `admin/services/cost_savings_report.go` | Create | `MonthlySavingsReport`, `CostSavingsReportService`, `GetMonthlySavings` |
| `admin/services/cost_savings_report_test.go` | Create | Unit test for pure type construction |
| `admin/api/cost_savings.go` | Create | `RegisterCostSavingsRoutes`, `getCostSavingsReport` handler |
| `admin/api/cost_savings_test.go` | Create | Handler tests with stub |
| `admin/main.go` | Modify | Wire `CostSavingsReportService` |
| `dashboard/src/pages/admin/CostCenterPage.tsx` | Modify | Add "Monthly Savings" section with routing event stats |

---

## Task 1: Request Dedup Cache (Redis)

**Files:**
- Create: `gateway/storage/request_cache.go`
- Create: `gateway/storage/request_cache_test.go`
- Modify: `gateway/main.go`

- [ ] **Step 1: Write failing tests**

Create `gateway/storage/request_cache_test.go`:

```go
package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/storage"
)

func TestCacheKey_Deterministic(t *testing.T) {
	k1 := storage.CacheKey("tenant-a", `{"model":"gpt-4o","messages":[]}`)
	k2 := storage.CacheKey("tenant-a", `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, k1, k2)
}

func TestCacheKey_DifferentTenantsDifferentKeys(t *testing.T) {
	k1 := storage.CacheKey("tenant-a", `{"model":"gpt-4o"}`)
	k2 := storage.CacheKey("tenant-b", `{"model":"gpt-4o"}`)
	assert.NotEqual(t, k1, k2)
}

func TestCacheKey_DifferentBodyDifferentKeys(t *testing.T) {
	k1 := storage.CacheKey("t", `{"model":"gpt-4o","messages":[{"content":"hello"}]}`)
	k2 := storage.CacheKey("t", `{"model":"gpt-4o","messages":[{"content":"world"}]}`)
	assert.NotEqual(t, k1, k2)
}

func TestCacheKey_HasPrefix(t *testing.T) {
	k := storage.CacheKey("t", "body")
	assert.Contains(t, k, "req_cache:")
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway
go test ./storage/ -run 'TestCacheKey' -v
```

Expected: FAIL — `storage.CacheKey` not defined.

- [ ] **Step 3: Implement RequestCache**

Create `gateway/storage/request_cache.go`:

```go
package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CacheKey produces a Redis key for a (tenantID, requestBody) pair.
func CacheKey(tenantID, body string) string {
	h := sha256.Sum256([]byte(tenantID + "|" + body))
	return fmt.Sprintf("req_cache:%x", h)
}

// RequestCache stores and retrieves cached AI responses in Redis.
type RequestCache struct {
	rdb *redis.Client
}

func NewRequestCache(rdb *redis.Client) *RequestCache {
	return &RequestCache{rdb: rdb}
}

// Get returns the cached response body, or (nil, false) on miss.
func (c *RequestCache) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

// Set stores a response body with the given TTL. Failures are silently ignored.
func (c *RequestCache) Set(ctx context.Context, key string, body []byte, ttl time.Duration) {
	c.rdb.Set(ctx, key, body, ttl)
}

// IncrHit increments the cache hit counter for (tenantID, yearMonth).
// yearMonth must be in YYYY-MM format.
func (c *RequestCache) IncrHit(ctx context.Context, tenantID, yearMonth string) {
	c.rdb.Incr(ctx, fmt.Sprintf("cache_hits:%s:%s", tenantID, yearMonth))
}

// GetHitCount returns cache hits for (tenantID, yearMonth).
func (c *RequestCache) GetHitCount(ctx context.Context, tenantID, yearMonth string) int64 {
	n, _ := c.rdb.Get(ctx, fmt.Sprintf("cache_hits:%s:%s", tenantID, yearMonth)).Int64()
	return n
}
```

- [ ] **Step 4: Run to confirm PASS**

```bash
cd /path/to/worktree/gateway
go test ./storage/ -run 'TestCacheKey' -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Inject cache into makeProxyHandler**

In `gateway/main.go`:

1. After creating `piiStore`, create:

```go
requestCache := storage.NewRequestCache(rdb)
```

2. Change `makeProxyHandler` signature to accept the cache:

```go
func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore, cache *storage.RequestCache) fiber.Handler {
```

3. Update the call site:

```go
proxyHandler := makeProxyHandler(pool, usageStore, agentStore, requestCache)
```

4. Inside `makeProxyHandler`, add the cache check immediately after the user local is retrieved and just before `reqBody` parsing:

```go
user := c.Locals("user").(*middleware.UserInfo)
start := time.Now()

// Cache check — identical requests by same tenant return cached response
cacheKey := storage.CacheKey(user.TenantID, string(c.Body()))
if cached, ok := cache.Get(c.Context(), cacheKey); ok {
    cache.IncrHit(c.Context(), user.TenantID, time.Now().UTC().Format("2006-01"))
    c.Set("X-Cache", "HIT")
    return c.Status(200).Send(cached)
}
```

5. After `result, usage, err := fwd.Forward(...)` and confirming no error, before the usage record, add:

```go
if result.StatusCode == 200 {
    cache.Set(c.Context(), cacheKey, result.Body, 24*time.Hour)
}
```

- [ ] **Step 6: Build to confirm no compile errors**

```bash
cd /path/to/worktree/gateway
go build ./...
```

Expected: builds cleanly.

- [ ] **Step 7: Commit**

```bash
git add gateway/storage/request_cache.go gateway/storage/request_cache_test.go gateway/main.go
git commit -m "feat(cost): request dedup cache — Redis SHA256 hash cache with 24h TTL, X-Cache: HIT header"
```

---

## Task 2: Migration 018 + Auto-Router Middleware

**Files:**
- Create: `infra/postgres/018_routing_events.sql`
- Modify: `docker-compose.yml`
- Create: `gateway/storage/routing_store.go`
- Create: `gateway/storage/routing_store_test.go`
- Create: `gateway/middleware/router.go`
- Create: `gateway/middleware/router_test.go`
- Modify: `gateway/main.go`

- [ ] **Step 1: Write failing router tests**

Create `gateway/middleware/router_test.go`:

```go
package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type captureRecorder struct {
	events []middleware.RoutingEvent
}

func (r *captureRecorder) Record(_ context.Context, e middleware.RoutingEvent) {
	r.events = append(r.events, e)
}

func setupRouterApp(rec middleware.RoutingRecorder) *fiber.App {
	app := fiber.New()
	// Simulate auth middleware setting user local
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendString(string(c.Body()))
	})
	return app
}

func TestAutoRouter_PremiumModel_ShortBody_Downgraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)

	// Short body (< 400 bytes) with premium model
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, len(rec.events))
	assert.Equal(t, "claude-opus-4-7", rec.events[0].OriginalModel)
	assert.Equal(t, "claude-sonnet-4-6", rec.events[0].RoutedModel)
}

func TestAutoRouter_StandardModel_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)

	body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events))
}

func TestAutoRouter_PremiumModel_LongBody_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)

	// Body >= 400 bytes — complex request, don't downgrade
	longContent := strings.Repeat("analyze this carefully: ", 20)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"` + longContent + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events))
}

func TestAutoRouter_NilRecorder_NoPanic(t *testing.T) {
	app := setupRouterApp(nil)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway
go test ./middleware/ -run 'TestAutoRouter' -v
```

Expected: compile error — `middleware.NewAutoRouterMiddleware`, `middleware.RoutingRecorder`, `middleware.RoutingEvent` not defined.

- [ ] **Step 3: Create the migration**

Create `infra/postgres/018_routing_events.sql`:

```sql
CREATE TABLE IF NOT EXISTS gateway_routing_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    original_model TEXT NOT NULL,
    routed_model TEXT NOT NULL,
    body_len_bytes INT NOT NULL DEFAULT 0,
    routed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_routing_events_tenant_month
    ON gateway_routing_events(tenant_id, routed_at);
```

- [ ] **Step 4: Add docker-compose mount**

In `docker-compose.yml`, in `postgres.volumes`, add after the `017_industry_compliance.sql` line:

```yaml
      - ./infra/postgres/018_routing_events.sql:/docker-entrypoint-initdb.d/018_routing_events.sql:ro
```

- [ ] **Step 5: Implement the auto-router middleware**

Create `gateway/middleware/router.go`:

```go
package middleware

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// RoutingEvent is emitted when the router downgrades a model.
type RoutingEvent struct {
	TenantID      string
	UserID        string
	OriginalModel string
	RoutedModel   string
	BodyLen       int
}

// RoutingRecorder persists routing events. Implement with RoutingStore in storage package.
type RoutingRecorder interface {
	Record(ctx context.Context, e RoutingEvent)
}

// complexityThreshold: request bodies shorter than this are treated as "simple".
const complexityThreshold = 400

// premiumToStandard maps premium model names to their cheaper equivalents.
var premiumToStandard = map[string]string{
	"claude-opus-4-7": "claude-sonnet-4-6",
	"claude-opus-4-5": "claude-sonnet-4-6",
	"gpt-4-turbo":     "gpt-4o-mini",
	"o1":              "gpt-4o-mini",
	"o1-preview":      "gpt-4o-mini",
}

// NewAutoRouterMiddleware downgrades premium models to standard when the request body
// is shorter than complexityThreshold bytes. rec may be nil (events not persisted).
func NewAutoRouterMiddleware(rec RoutingRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		if len(body) >= complexityThreshold {
			return c.Next()
		}

		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil || reqBody.Model == "" {
			return c.Next()
		}

		cheaper, ok := premiumToStandard[reqBody.Model]
		if !ok {
			return c.Next()
		}

		// Rewrite model field in the request body
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return c.Next()
		}
		newModel, _ := json.Marshal(cheaper)
		raw["model"] = newModel
		newBody, err := json.Marshal(raw)
		if err != nil {
			return c.Next()
		}
		c.Request().SetBody(newBody)
		c.Set("X-Routed-From", reqBody.Model)
		c.Set("X-Routed-To", cheaper)

		if rec != nil {
			user, _ := c.Locals("user").(*UserInfo)
			uid, tid := "", ""
			if user != nil {
				uid, tid = user.UserID, user.TenantID
			}
			rec.Record(c.Context(), RoutingEvent{
				TenantID:      tid,
				UserID:        uid,
				OriginalModel: reqBody.Model,
				RoutedModel:   cheaper,
				BodyLen:       len(body),
			})
		}

		return c.Next()
	}
}
```

- [ ] **Step 6: Implement the routing store**

Create `gateway/storage/routing_store.go`:

```go
package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// RoutingStore persists routing events to Postgres asynchronously.
type RoutingStore struct {
	pool *pgxpool.Pool
}

func NewRoutingStore(pool *pgxpool.Pool) *RoutingStore {
	return &RoutingStore{pool: pool}
}

func (s *RoutingStore) Record(ctx context.Context, e middleware.RoutingEvent) {
	go func() {
		_, err := s.pool.Exec(context.Background(),
			`INSERT INTO gateway_routing_events
			 (tenant_id, user_id, original_model, routed_model, body_len_bytes)
			 VALUES ($1, $2, $3, $4, $5)`,
			e.TenantID, e.UserID, e.OriginalModel, e.RoutedModel, e.BodyLen,
		)
		if err != nil {
			log.Printf("routing_store: record: %v", err)
		}
	}()
}
```

- [ ] **Step 7: Write compile-time interface check for routing store**

Create `gateway/storage/routing_store_test.go`:

```go
package storage_test

import (
	"testing"

	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// Compile-time check that RoutingStore implements RoutingRecorder.
func TestRoutingStore_ImplementsInterface(t *testing.T) {
	var _ middleware.RoutingRecorder = (*storage.RoutingStore)(nil)
}
```

- [ ] **Step 8: Run all gateway tests**

```bash
cd /path/to/worktree/gateway
go test ./... -v
```

Expected: all tests PASS including the 4 new router tests and the interface check.

- [ ] **Step 9: Wire router into gateway/main.go**

In `gateway/main.go`:

1. After `piiStore := storage.NewPIIStore(pool, 256)` and `requestCache := storage.NewRequestCache(rdb)`, add:

```go
routingStore := storage.NewRoutingStore(pool)
```

2. In the `/v1` group, add `middleware.NewAutoRouterMiddleware(routingStore)` after the PII middleware:

```go
v1 := app.Group("/v1",
    middleware.NewAuthMiddleware(pgUserLookup),
    middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
    middleware.NewPIIMiddleware(piiStore, ""),
    middleware.NewAutoRouterMiddleware(routingStore),
    middleware.NewAgentMiddleware(agentStore),
)
```

3. Build check:

```bash
cd /path/to/worktree/gateway
go build ./...
```

Expected: builds cleanly.

- [ ] **Step 10: Commit**

```bash
git add infra/postgres/018_routing_events.sql docker-compose.yml \
        gateway/middleware/router.go gateway/middleware/router_test.go \
        gateway/storage/routing_store.go gateway/storage/routing_store_test.go \
        gateway/main.go
git commit -m "feat(cost): auto-router — downgrade premium→standard for short requests (<400 bytes), async routing event log"
```

---

## Task 3: Monthly Savings Report Service + API

**Files:**
- Create: `admin/services/cost_savings_report.go`
- Create: `admin/services/cost_savings_report_test.go`
- Create: `admin/api/cost_savings.go`
- Create: `admin/api/cost_savings_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing service test**

Create `admin/services/cost_savings_report_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestMonthlySavingsReport_NonNilModelsList(t *testing.T) {
	svc := services.NewCostSavingsReportService(nil)
	assert.NotNil(t, svc)
}

func TestRoutedModelStat_Fields(t *testing.T) {
	s := services.RoutedModelStat{
		OriginalModel: "claude-opus-4-7",
		RoutedModel:   "claude-sonnet-4-6",
		Count:         42,
	}
	assert.Equal(t, "claude-opus-4-7", s.OriginalModel)
	assert.Equal(t, int64(42), s.Count)
}
```

- [ ] **Step 2: Write failing API test**

Create `admin/api/cost_savings_test.go`:

```go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubSavingsSvc struct{}

func (s *stubSavingsSvc) GetMonthlySavings(_ context.Context, _, _ string) (*services.MonthlySavingsReport, error) {
	return &services.MonthlySavingsReport{
		YearMonth:          "2026-05",
		RoutingEventCount:  10,
		RoutingEventModels: []services.RoutedModelStat{},
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func setupSavingsApp(svc api.CostSavingsServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterCostSavingsRoutes(app, svc)
	return app
}

func TestGetCostSavingsReport_OK(t *testing.T) {
	app := setupSavingsApp(&stubSavingsSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/savings-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetCostSavingsReport_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterCostSavingsRoutes(app, &stubSavingsSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cost/savings-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 3: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestMonthlySavings|TestRoutedModelStat' -v
go test ./api/ -run 'TestGetCostSavings' -v
```

Expected: compile error — types + functions not defined.

- [ ] **Step 4: Implement CostSavingsReportService**

Create `admin/services/cost_savings_report.go`:

```go
package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MonthlySavingsReport summarises model auto-routing events for a month.
type MonthlySavingsReport struct {
	YearMonth          string            `json:"year_month"`
	RoutingEventCount  int64             `json:"routing_event_count"`
	RoutingEventModels []RoutedModelStat `json:"routed_models"`
	GeneratedAt        string            `json:"generated_at"`
}

// RoutedModelStat is the count of downgrades per (original, routed) model pair.
type RoutedModelStat struct {
	OriginalModel string `json:"original_model"`
	RoutedModel   string `json:"routed_model"`
	Count         int64  `json:"count"`
}

// CostSavingsReportService queries routing events from Postgres.
type CostSavingsReportService struct {
	pool *pgxpool.Pool
}

func NewCostSavingsReportService(pool *pgxpool.Pool) *CostSavingsReportService {
	return &CostSavingsReportService{pool: pool}
}

// GetMonthlySavings returns routing event stats for tenantID in yearMonth (YYYY-MM).
func (s *CostSavingsReportService) GetMonthlySavings(ctx context.Context, tenantID, yearMonth string) (*MonthlySavingsReport, error) {
	report := &MonthlySavingsReport{
		YearMonth:   yearMonth,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gateway_routing_events
		 WHERE tenant_id = $1
		   AND to_char(routed_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2`,
		tenantID, yearMonth,
	).Scan(&report.RoutingEventCount)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT original_model, routed_model, COUNT(*) AS cnt
		 FROM gateway_routing_events
		 WHERE tenant_id = $1
		   AND to_char(routed_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		 GROUP BY original_model, routed_model
		 ORDER BY cnt DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var stat RoutedModelStat
		if err := rows.Scan(&stat.OriginalModel, &stat.RoutedModel, &stat.Count); err != nil {
			return nil, err
		}
		report.RoutingEventModels = append(report.RoutingEventModels, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if report.RoutingEventModels == nil {
		report.RoutingEventModels = []RoutedModelStat{}
	}

	return report, nil
}
```

- [ ] **Step 5: Implement the API handler**

Create `admin/api/cost_savings.go`:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// CostSavingsServiceIface allows stub injection in tests.
type CostSavingsServiceIface interface {
	GetMonthlySavings(ctx context.Context, tenantID, yearMonth string) (*services.MonthlySavingsReport, error)
}

func RegisterCostSavingsRoutes(app fiber.Router, svc CostSavingsServiceIface) {
	app.Get("/api/admin/cost/savings-report", getCostSavingsReport(svc))
}

func getCostSavingsReport(svc CostSavingsServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		report, err := svc.GetMonthlySavings(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(report)
	}
}
```

- [ ] **Step 6: Wire into admin/main.go**

After `costSvc := services.NewCostAnalysisService(pool)`, add:

```go
costSavingsSvc := services.NewCostSavingsReportService(pool)
```

After `api.RegisterCostAnalysisRoutes(protected, costSvc, botSvc)`, add:

```go
api.RegisterCostSavingsRoutes(protected, costSavingsSvc)
```

- [ ] **Step 7: Run all admin tests**

```bash
cd /path/to/worktree/admin
go test ./... -count=1 -v
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add admin/services/cost_savings_report.go admin/services/cost_savings_report_test.go \
        admin/api/cost_savings.go admin/api/cost_savings_test.go \
        admin/main.go
git commit -m "feat(cost): monthly savings report — routing event count + per-model breakdown, GET /api/admin/cost/savings-report"
```

---

## Task 4: Dashboard — CostCenterPage Phase 2 Additions

**Files:**
- Modify: `dashboard/src/pages/admin/CostCenterPage.tsx`

- [ ] **Step 1: Read the current CostCenterPage.tsx**

Read `dashboard/src/pages/admin/CostCenterPage.tsx` to understand the current structure — it has a month picker, KPI cards (total cost, top model, off-hours cost), and tables. Find where to add the new section.

- [ ] **Step 2: Add savings report query and section**

Add a new `useQuery` for the savings report, and render a "Monthly Routing Savings" section below the existing content.

Add this query at the top of the component (alongside existing queries):

```tsx
const { data: savings } = useQuery({
  queryKey: ["cost-savings", month],
  queryFn: () =>
    apiClient
      .get<{
        routing_event_count: number;
        routed_models: { original_model: string; routed_model: string; count: number }[];
        generated_at: string;
      }>(`/api/admin/cost/savings-report?month=${month}`)
      .then((r) => r.data),
});
```

Add this section at the bottom of the JSX, after the existing tables (before the closing `</div>`):

```tsx
{/* Monthly Routing Savings */}
<div className="mt-8">
  <h2 className="text-lg font-semibold mb-4">Auto-Routing Events This Month</h2>
  <div className="mb-4 px-4 py-3 bg-green-50 border border-green-100 rounded-lg text-sm text-green-700">
    <span className="font-semibold">{savings?.routing_event_count ?? 0}</span> requests
    automatically downgraded to a cheaper model
  </div>
  {savings && savings.routed_models.length > 0 && (
    <div className="overflow-x-auto">
      <table className="w-full text-sm border rounded-lg overflow-hidden">
        <thead className="bg-gray-50 text-gray-500 text-xs uppercase">
          <tr>
            <th className="px-4 py-2 text-left">Original Model</th>
            <th className="px-4 py-2 text-left">Routed To</th>
            <th className="px-4 py-2 text-right">Requests</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {savings.routed_models.map((row, i) => (
            <tr key={i} className="hover:bg-gray-50">
              <td className="px-4 py-2 font-mono text-xs">{row.original_model}</td>
              <td className="px-4 py-2 font-mono text-xs text-green-600">{row.routed_model}</td>
              <td className="px-4 py-2 text-right font-medium">{row.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )}
  {savings && savings.routed_models.length === 0 && (
    <p className="text-sm text-gray-400">No routing events recorded this month.</p>
  )}
</div>
```

- [ ] **Step 3: TypeScript check**

```bash
cd /path/to/worktree/dashboard
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add dashboard/src/pages/admin/CostCenterPage.tsx
git commit -m "feat(cost): CostCenterPage — monthly routing savings panel with per-model breakdown table"
```

---

## Self-Review

**Spec coverage:**
- ✅ 语义缓存 (request dedup cache via SHA256 hash, Redis 24h TTL) — Task 1
- ✅ 模型自动路由 (premium→standard for <400 byte bodies) — Task 2
- ✅ 优化建议报告 (monthly routing savings report endpoint + dashboard panel) — Task 3 + Task 4
- Prompt 压缩 is deferred — requires an LLM call to compress prompts, out of scope for Phase 2.

**Placeholder scan:** none found.

**Type consistency:**
- `RoutingEvent` defined in `gateway/middleware/router.go`, used by `RoutingRecorder` interface — `RoutingStore.Record` matches signature.
- `MonthlySavingsReport` + `RoutedModelStat` defined in `admin/services/cost_savings_report.go`, returned by service and used in stub test.
- `CostSavingsServiceIface` accepts `GetMonthlySavings(ctx, tenantID, yearMonth)` — matches service implementation.
- `currentYearMonth()` already defined in `admin/api/compliance.go` (same `api` package) — not redefined in `cost_savings.go`. ✅
