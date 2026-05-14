# Compliance Platform Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Custom Policy Rules Engine — tenants define per-tenant regex block/log rules stored in DB, loaded by the gateway via Redis cache, enforced alongside built-in PII patterns. Admin CRUD API + Dashboard management page.

**Architecture:** New `tenant_policy_rules` table (migration 019). Gateway gets a `PolicyRuleStore` (DB→Redis, 5-min TTL) and `PolicyMiddleware` inserted after `PIIMiddleware`. Admin gets a service + 4 REST endpoints. Dashboard gets a new Policy Rules page.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5 + go-redis/v9 (gateway/admin) · React 19 + TanStack Query v5 + Tailwind v4 (dashboard)

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `infra/postgres/019_policy_rules.sql` | `tenant_policy_rules` table |
| Modify | `docker-compose.yml` | Mount migration 019 |
| Create | `gateway/storage/policy_store.go` | Load rules from DB, cache in Redis |
| Create | `gateway/storage/policy_store_test.go` | Unit tests |
| Create | `gateway/middleware/policy.go` | PolicyMiddleware |
| Create | `gateway/middleware/policy_test.go` | Handler tests |
| Modify | `gateway/main.go` | Wire PolicyRuleStore + PolicyMiddleware |
| Create | `admin/services/policy_rules.go` | CRUD service + regex validation |
| Create | `admin/services/policy_rules_test.go` | Unit tests |
| Create | `admin/api/policy_rules.go` | 4 REST endpoints |
| Create | `admin/api/policy_rules_test.go` | Handler tests |
| Modify | `admin/main.go` | Wire PolicyRulesService |
| Create | `dashboard/src/pages/admin/PolicyRulesPage.tsx` | List/create/edit/delete rules |
| Modify | `dashboard/src/App.tsx` | Add policy-rules route |
| Modify | `dashboard/src/components/Layout.tsx` | Add nav item |

---

### Task 1: DB Migration + docker-compose

**Files:**
- Create: `infra/postgres/019_policy_rules.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Create migration**

```sql
-- infra/postgres/019_policy_rules.sql
CREATE TABLE IF NOT EXISTS tenant_policy_rules (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    pattern     TEXT        NOT NULL,
    action      TEXT        NOT NULL DEFAULT 'block',
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_policy_rules_tenant_active
    ON tenant_policy_rules(tenant_id, is_active);
```

- [ ] **Step 2: Mount in docker-compose.yml**

In the `postgres` service volumes block, after the line mounting `018_routing_events.sql`, add:

```yaml
      - ./infra/postgres/019_policy_rules.sql:/docker-entrypoint-initdb.d/019_policy_rules.sql:ro
```

- [ ] **Step 3: Commit**

```bash
git add infra/postgres/019_policy_rules.sql docker-compose.yml
git commit -m "feat(compliance): migration 019 — tenant_policy_rules table"
```

---

### Task 2: Gateway PolicyRuleStore + PolicyMiddleware

**Files:**
- Create: `gateway/storage/policy_store.go`
- Create: `gateway/storage/policy_store_test.go`
- Create: `gateway/middleware/policy.go`
- Create: `gateway/middleware/policy_test.go`
- Modify: `gateway/main.go`

- [ ] **Step 1: Write failing tests for PolicyMiddleware**

```go
// gateway/middleware/policy_test.go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type stubPolicyStore struct {
	rules []*middleware.PolicyRule
}

func (s *stubPolicyStore) GetRules(_ interface{}, _ string) ([]*middleware.PolicyRule, error) {
	return s.rules, nil
}

func setupPolicyApp(rules []*middleware.PolicyRule) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	store := &stubPolicyStore{rules: rules}
	app.Use(middleware.NewPolicyMiddleware(store))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	return app
}

func TestPolicyMiddleware_NoRules(t *testing.T) {
	app := setupPolicyApp(nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyMiddleware_BlockRule_Matches(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"SSN: 123-45-6789"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPolicyMiddleware_BlockRule_NoMatch(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hello world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyMiddleware_LogRule_Passes(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "log-keyword", Pattern: `secret`, Action: "log"},
	}
	app := setupPolicyApp(rules)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"top secret project"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./middleware/ -run TestPolicyMiddleware -v 2>&1 | head -15
```

Expected: compile error — `middleware.PolicyRule` undefined.

- [ ] **Step 3: Implement PolicyRuleStore**

```go
// gateway/storage/policy_store.go
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/middleware"
)

const policyRuleCacheTTL = 5 * time.Minute

type PolicyRuleStore struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func NewPolicyRuleStore(pool *pgxpool.Pool, rdb *redis.Client) *PolicyRuleStore {
	return &PolicyRuleStore{pool: pool, rdb: rdb}
}

func (s *PolicyRuleStore) GetRules(ctx context.Context, tenantID string) ([]*middleware.PolicyRule, error) {
	cacheKey := fmt.Sprintf("policy:%s", tenantID)

	if cached, err := s.rdb.Get(ctx, cacheKey).Bytes(); err == nil {
		var rules []*middleware.PolicyRule
		if json.Unmarshal(cached, &rules) == nil {
			return rules, nil
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT name, pattern, action
		FROM tenant_policy_rules
		WHERE tenant_id = $1 AND is_active = TRUE
		ORDER BY id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("policy rules query: %w", err)
	}
	defer rows.Close()

	var rules []*middleware.PolicyRule
	for rows.Next() {
		r := &middleware.PolicyRule{}
		if err := rows.Scan(&r.Name, &r.Pattern, &r.Action); err != nil {
			return nil, fmt.Errorf("policy rules scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy rules rows: %w", err)
	}

	if data, err := json.Marshal(rules); err == nil {
		s.rdb.Set(ctx, cacheKey, data, policyRuleCacheTTL)
	}
	return rules, nil
}

// InvalidateCache removes cached policy rules for a tenant (call after CRUD operations).
func (s *PolicyRuleStore) InvalidateCache(ctx context.Context, tenantID string) {
	s.rdb.Del(ctx, fmt.Sprintf("policy:%s", tenantID))
}
```

- [ ] **Step 4: Implement PolicyMiddleware**

```go
// gateway/middleware/policy.go
package middleware

import (
	"context"
	"log"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

// PolicyRule is a tenant-defined content rule.
type PolicyRule struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"` // "block" or "log"
}

// PolicyRuleGetter loads active rules for a tenant.
type PolicyRuleGetter interface {
	GetRules(ctx context.Context, tenantID string) ([]*PolicyRule, error)
}

// NewPolicyMiddleware enforces tenant-configured content rules.
func NewPolicyMiddleware(store PolicyRuleGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}

		rules, err := store.GetRules(c.Context(), user.TenantID)
		if err != nil {
			log.Printf("policy middleware: load rules error: %v", err)
			return c.Next()
		}

		body := string(c.Body())
		for _, rule := range rules {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			if re.MatchString(body) {
				if rule.Action == "block" {
					return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
						"error": fiber.Map{
							"message": "request blocked by policy rule: " + rule.Name,
							"type":    "policy_blocked",
						},
					})
				}
				// action == "log": pass through, just log
				log.Printf("policy rule matched (log): tenant=%s rule=%s", user.TenantID, rule.Name)
			}
		}
		return c.Next()
	}
}
```

- [ ] **Step 5: Run middleware tests to verify they pass**

```bash
cd /path/to/worktree/gateway && go test ./middleware/ -run TestPolicyMiddleware -v
```

Expected: all 4 tests PASS.

- [ ] **Step 6: Wire into gateway/main.go**

Read `gateway/main.go`. After `routingStore := storage.NewRoutingStore(pool)`, add:

```go
policyRuleStore := storage.NewPolicyRuleStore(pool, rdb)
```

In the `v1` group, after `middleware.NewPIIMiddleware(piiStore, "")`, add:

```go
middleware.NewPolicyMiddleware(policyRuleStore),
```

- [ ] **Step 7: Build gateway**

```bash
cd /path/to/worktree/gateway && go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add gateway/storage/policy_store.go gateway/storage/policy_store_test.go \
        gateway/middleware/policy.go gateway/middleware/policy_test.go \
        gateway/main.go
git commit -m "feat(compliance): gateway policy middleware — per-tenant custom block/log rules with Redis cache"
```

---

### Task 3: Admin Policy Rules Service + API

**Files:**
- Create: `admin/services/policy_rules.go`
- Create: `admin/services/policy_rules_test.go`
- Create: `admin/api/policy_rules.go`
- Create: `admin/api/policy_rules_test.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing unit tests**

```go
// admin/services/policy_rules_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestValidatePattern_Valid(t *testing.T) {
	assert.NoError(t, services.ValidatePattern(`\b\d{3}-\d{2}-\d{4}\b`))
	assert.NoError(t, services.ValidatePattern(`(?i)confidential`))
}

func TestValidatePattern_Invalid(t *testing.T) {
	assert.Error(t, services.ValidatePattern(`[invalid`))
	assert.Error(t, services.ValidatePattern(`(`))
}

func TestValidateAction_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateAction("block"))
	assert.NoError(t, services.ValidateAction("log"))
}

func TestValidateAction_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateAction("deny"))
	assert.Error(t, services.ValidateAction(""))
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./services/ -run TestValidate -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 3: Implement service**

```go
// admin/services/policy_rules.go
package services

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ValidatePattern(pattern string) error {
	_, err := regexp.Compile(pattern)
	return err
}

func ValidateAction(action string) error {
	if action == "block" || action == "log" {
		return nil
	}
	return fmt.Errorf("action must be 'block' or 'log', got %q", action)
}

type PolicyRule struct {
	ID        int64   `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Name      string  `json:"name"`
	Pattern   string  `json:"pattern"`
	Action    string  `json:"action"`
	IsActive  bool    `json:"is_active"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type PolicyRulesService struct{ pool *pgxpool.Pool }

func NewPolicyRulesService(pool *pgxpool.Pool) *PolicyRulesService {
	return &PolicyRulesService{pool: pool}
}

func (s *PolicyRulesService) List(ctx context.Context, tenantID string) ([]*PolicyRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, pattern, action, is_active, created_at, updated_at
		FROM tenant_policy_rules WHERE tenant_id = $1 ORDER BY id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("policy rules list: %w", err)
	}
	defer rows.Close()
	var rules []*PolicyRule
	for rows.Next() {
		r := &PolicyRule{}
		var ca, ua time.Time
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua); err != nil {
			return nil, err
		}
		r.CreatedAt = ca.UTC().Format(time.RFC3339)
		r.UpdatedAt = ua.UTC().Format(time.RFC3339)
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []*PolicyRule{}
	}
	return rules, nil
}

func (s *PolicyRulesService) Create(ctx context.Context, tenantID, name, pattern, action string) (*PolicyRule, error) {
	if err := ValidatePattern(pattern); err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	if err := ValidateAction(action); err != nil {
		return nil, err
	}
	var r PolicyRule
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_policy_rules(tenant_id, name, pattern, action)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, name, pattern, action, is_active, created_at, updated_at
	`, tenantID, name, pattern, action).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua,
	)
	if err != nil {
		return nil, fmt.Errorf("policy rule create: %w", err)
	}
	r.CreatedAt = ca.UTC().Format(time.RFC3339)
	r.UpdatedAt = ua.UTC().Format(time.RFC3339)
	return &r, nil
}

func (s *PolicyRulesService) Update(ctx context.Context, tenantID string, id int64, name, pattern, action string, isActive bool) (*PolicyRule, error) {
	if err := ValidatePattern(pattern); err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	if err := ValidateAction(action); err != nil {
		return nil, err
	}
	var r PolicyRule
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE tenant_policy_rules
		SET name=$3, pattern=$4, action=$5, is_active=$6, updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
		RETURNING id, tenant_id, name, pattern, action, is_active, created_at, updated_at
	`, id, tenantID, name, pattern, action, isActive).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua,
	)
	if err != nil {
		return nil, fmt.Errorf("policy rule update: %w", err)
	}
	r.CreatedAt = ca.UTC().Format(time.RFC3339)
	r.UpdatedAt = ua.UTC().Format(time.RFC3339)
	return &r, nil
}

func (s *PolicyRulesService) Delete(ctx context.Context, tenantID string, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tenant_policy_rules WHERE id=$1 AND tenant_id=$2
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("policy rule delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}
```

- [ ] **Step 4: Run service unit tests**

```bash
cd /path/to/worktree/admin && go test ./services/ -run TestValidate -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Write failing handler tests**

```go
// admin/api/policy_rules_test.go
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

type stubPolicyRulesSvc struct{}

func (s *stubPolicyRulesSvc) List(_ context.Context, _ string) ([]*services.PolicyRule, error) {
	return []*services.PolicyRule{{ID: 1, Name: "test", Pattern: `\d+`, Action: "block", IsActive: true}}, nil
}
func (s *stubPolicyRulesSvc) Create(_ context.Context, _, name, pattern, action string) (*services.PolicyRule, error) {
	return &services.PolicyRule{ID: 2, Name: name, Pattern: pattern, Action: action, IsActive: true}, nil
}
func (s *stubPolicyRulesSvc) Update(_ context.Context, _ string, id int64, name, pattern, action string, isActive bool) (*services.PolicyRule, error) {
	return &services.PolicyRule{ID: id, Name: name, Pattern: pattern, Action: action, IsActive: isActive}, nil
}
func (s *stubPolicyRulesSvc) Delete(_ context.Context, _ string, _ int64) error { return nil }

func makePolicyApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u1", TenantID: "t1", Role: "admin"})
		return c.Next()
	})
	api.RegisterPolicyRulesRoutes(app, &stubPolicyRulesSvc{})
	return app
}

func TestPolicyRules_List(t *testing.T) {
	resp, err := makePolicyApp().Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/policy-rules", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPolicyRules_Create(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"name": "r1", "pattern": `\d+`, "action": "block"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/policy-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := makePolicyApp().Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestPolicyRules_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterPolicyRulesRoutes(app, &stubPolicyRulesSvc{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/compliance/policy-rules", nil))
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 6: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin && go test ./api/ -run TestPolicyRules -v 2>&1 | head -10
```

Expected: compile error.

- [ ] **Step 7: Implement API handler**

```go
// admin/api/policy_rules.go
package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type PolicyRulesServiceIface interface {
	List(ctx context.Context, tenantID string) ([]*services.PolicyRule, error)
	Create(ctx context.Context, tenantID, name, pattern, action string) (*services.PolicyRule, error)
	Update(ctx context.Context, tenantID string, id int64, name, pattern, action string, isActive bool) (*services.PolicyRule, error)
	Delete(ctx context.Context, tenantID string, id int64) error
}

func RegisterPolicyRulesRoutes(r fiber.Router, svc PolicyRulesServiceIface) {
	r.Get("/api/admin/compliance/policy-rules", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		rules, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(rules)
	})

	r.Post("/api/admin/compliance/policy-rules", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Name    string `json:"name"`
			Pattern string `json:"pattern"`
			Action  string `json:"action"`
		}
		if err := c.BodyParser(&body); err != nil || body.Name == "" || body.Pattern == "" || body.Action == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name, pattern, action required"})
		}
		rule, err := svc.Create(c.Context(), claims.TenantID, body.Name, body.Pattern, body.Action)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(rule)
	})

	r.Put("/api/admin/compliance/policy-rules/:id", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
		}
		var body struct {
			Name     string `json:"name"`
			Pattern  string `json:"pattern"`
			Action   string `json:"action"`
			IsActive bool   `json:"is_active"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		rule, err := svc.Update(c.Context(), claims.TenantID, id, body.Name, body.Pattern, body.Action, body.IsActive)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(rule)
	})

	r.Delete("/api/admin/compliance/policy-rules/:id", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(204)
	})
}
```

- [ ] **Step 8: Wire into admin/main.go**

Read `admin/main.go`. After `checklistSvc` wiring (or any compliance-related service wiring), add:

```go
policyRulesSvc := services.NewPolicyRulesService(pool)
```

And in the protected routes section, after `RegisterComplianceAdvancedRoutes`:

```go
api.RegisterPolicyRulesRoutes(protected, policyRulesSvc)
```

- [ ] **Step 9: Run all admin tests**

```bash
cd /path/to/worktree/admin && go test ./... -count=1
```

Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add admin/services/policy_rules.go admin/services/policy_rules_test.go \
        admin/api/policy_rules.go admin/api/policy_rules_test.go \
        admin/main.go
git commit -m "feat(compliance): PolicyRulesService + CRUD API — tenant custom block/log rules"
```

---

### Task 4: Dashboard Policy Rules Page

**Files:**
- Create: `dashboard/src/pages/admin/PolicyRulesPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

Read all three files at their absolute paths in the worktree before editing.

- [ ] **Step 1: TypeScript baseline check**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit 2>&1 | head -10
```

Expected: 0 errors.

- [ ] **Step 2: Create PolicyRulesPage.tsx**

Follow the exact same import pattern as other admin pages (apiClient, useQuery/useMutation from TanStack Query v5, auth token from the existing pattern in other pages).

```tsx
// dashboard/src/pages/admin/PolicyRulesPage.tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface PolicyRule {
  id: number;
  name: string;
  pattern: string;
  action: "block" | "log";
  is_active: boolean;
  created_at: string;
}

export default function PolicyRulesPage() {
  const qc = useQueryClient();
  const [form, setForm] = useState({ name: "", pattern: "", action: "block" });
  const [editId, setEditId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState<Partial<PolicyRule>>({});

  const { data: rules = [], isLoading } = useQuery({
    queryKey: ["policy-rules"],
    queryFn: () =>
      apiClient
        .get<PolicyRule[]>("/api/admin/compliance/policy-rules")
        .then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: (body: { name: string; pattern: string; action: string }) =>
      apiClient.post("/api/admin/compliance/policy-rules", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy-rules"] });
      setForm({ name: "", pattern: "", action: "block" });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, ...body }: Partial<PolicyRule> & { id: number }) =>
      apiClient.put(`/api/admin/compliance/policy-rules/${id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy-rules"] });
      setEditId(null);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) =>
      apiClient.delete(`/api/admin/compliance/policy-rules/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policy-rules"] }),
  });

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-1">Policy Rules</h1>
      <p className="text-gray-500 text-sm mb-6">
        Custom content rules applied to all AI requests for your tenant. Rules are enforced in addition to built-in PII detection.
      </p>

      {/* Create form */}
      <div className="bg-white rounded-xl shadow p-6 mb-6">
        <h2 className="text-base font-semibold mb-4">Add Rule</h2>
        <div className="flex gap-3 flex-wrap">
          <input
            className="border rounded px-3 py-2 text-sm flex-1 min-w-[140px]"
            placeholder="Rule name"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
          <input
            className="border rounded px-3 py-2 text-sm flex-[2] min-w-[200px] font-mono"
            placeholder="Regex pattern (e.g. \bconfidential\b)"
            value={form.pattern}
            onChange={(e) => setForm((f) => ({ ...f, pattern: e.target.value }))}
          />
          <select
            className="border rounded px-3 py-2 text-sm"
            value={form.action}
            onChange={(e) => setForm((f) => ({ ...f, action: e.target.value }))}
          >
            <option value="block">Block</option>
            <option value="log">Log only</option>
          </select>
          <button
            className="bg-blue-600 text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
            disabled={!form.name || !form.pattern || createMutation.isPending}
            onClick={() => createMutation.mutate(form)}
          >
            Add
          </button>
        </div>
        {createMutation.isError && (
          <p className="text-red-600 text-sm mt-2">
            {(createMutation.error as Error)?.message ?? "Failed to create rule"}
          </p>
        )}
      </div>

      {/* Rules list */}
      <div className="bg-white rounded-xl shadow overflow-hidden">
        {isLoading ? (
          <p className="p-6 text-gray-400">Loading…</p>
        ) : rules.length === 0 ? (
          <p className="p-6 text-gray-400">No policy rules defined yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-gray-50 border-b">
              <tr>
                <th className="text-left px-4 py-3 text-gray-500 font-medium">Name</th>
                <th className="text-left px-4 py-3 text-gray-500 font-medium">Pattern</th>
                <th className="text-left px-4 py-3 text-gray-500 font-medium">Action</th>
                <th className="text-left px-4 py-3 text-gray-500 font-medium">Status</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {rules.map((rule) => (
                <tr key={rule.id} className="border-b last:border-0">
                  {editId === rule.id ? (
                    <>
                      <td className="px-4 py-2">
                        <input
                          className="border rounded px-2 py-1 text-sm w-full"
                          value={editForm.name ?? rule.name}
                          onChange={(e) => setEditForm((f) => ({ ...f, name: e.target.value }))}
                        />
                      </td>
                      <td className="px-4 py-2">
                        <input
                          className="border rounded px-2 py-1 text-sm w-full font-mono"
                          value={editForm.pattern ?? rule.pattern}
                          onChange={(e) => setEditForm((f) => ({ ...f, pattern: e.target.value }))}
                        />
                      </td>
                      <td className="px-4 py-2">
                        <select
                          className="border rounded px-2 py-1 text-sm"
                          value={editForm.action ?? rule.action}
                          onChange={(e) => setEditForm((f) => ({ ...f, action: e.target.value as "block" | "log" }))}
                        >
                          <option value="block">Block</option>
                          <option value="log">Log only</option>
                        </select>
                      </td>
                      <td className="px-4 py-2">
                        <input
                          type="checkbox"
                          checked={editForm.is_active ?? rule.is_active}
                          onChange={(e) => setEditForm((f) => ({ ...f, is_active: e.target.checked }))}
                        />
                        <span className="ml-2 text-xs text-gray-500">Active</span>
                      </td>
                      <td className="px-4 py-2 flex gap-2">
                        <button
                          className="text-blue-600 text-xs font-medium"
                          onClick={() =>
                            updateMutation.mutate({
                              id: rule.id,
                              name: editForm.name ?? rule.name,
                              pattern: editForm.pattern ?? rule.pattern,
                              action: editForm.action ?? rule.action,
                              is_active: editForm.is_active ?? rule.is_active,
                            })
                          }
                        >
                          Save
                        </button>
                        <button className="text-gray-400 text-xs" onClick={() => setEditId(null)}>
                          Cancel
                        </button>
                      </td>
                    </>
                  ) : (
                    <>
                      <td className="px-4 py-3 font-medium">{rule.name}</td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{rule.pattern}</td>
                      <td className="px-4 py-3">
                        <span
                          className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                            rule.action === "block"
                              ? "bg-red-50 text-red-700"
                              : "bg-yellow-50 text-yellow-700"
                          }`}
                        >
                          {rule.action}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={`px-2 py-0.5 rounded-full text-xs ${
                            rule.is_active ? "bg-green-50 text-green-700" : "bg-gray-100 text-gray-500"
                          }`}
                        >
                          {rule.is_active ? "Active" : "Inactive"}
                        </span>
                      </td>
                      <td className="px-4 py-3 flex gap-3 justify-end">
                        <button
                          className="text-blue-600 text-xs font-medium"
                          onClick={() => { setEditId(rule.id); setEditForm({}); }}
                        >
                          Edit
                        </button>
                        <button
                          className="text-red-500 text-xs font-medium"
                          onClick={() => deleteMutation.mutate(rule.id)}
                        >
                          Delete
                        </button>
                      </td>
                    </>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Add route to App.tsx**

Import after `ComplianceAnomalyPage`:

```tsx
import PolicyRulesPage from "./pages/admin/PolicyRulesPage";
```

Add route after `admin/compliance/anomalies`:

```tsx
<Route path="admin/compliance/policy-rules" element={<ProtectedRoute adminOnly><PolicyRulesPage /></ProtectedRoute>} />
```

- [ ] **Step 4: Add nav item to Layout.tsx**

In `adminNavItems`, after `{ label: "Anomaly Alerts", href: "/admin/compliance/anomalies" }`:

```tsx
{ label: "Policy Rules", href: "/admin/compliance/policy-rules" },
```

- [ ] **Step 5: TypeScript check**

```bash
cd /path/to/worktree/dashboard && npx tsc --noEmit
```

Fix any errors before committing.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/admin/PolicyRulesPage.tsx \
        dashboard/src/App.tsx \
        dashboard/src/components/Layout.tsx
git commit -m "feat(compliance): Policy Rules page — list/create/edit/delete tenant custom rules"
```

---

## Self-Review

**Spec coverage:**
- ✅ DB table `tenant_policy_rules` with index — Task 1
- ✅ `docker-compose.yml` updated — Task 1
- ✅ Gateway `PolicyRuleStore` (DB + Redis 5-min cache) — Task 2
- ✅ `PolicyMiddleware` (block → 422, log → pass-through) — Task 2
- ✅ Gateway wired — Task 2
- ✅ `ValidatePattern` / `ValidateAction` pure functions, tested — Task 3
- ✅ Admin service: List/Create/Update/Delete — Task 3
- ✅ 4 REST endpoints, admin-guarded — Task 3
- ✅ Admin wired — Task 3
- ✅ Dashboard page: create/edit/delete/toggle — Task 4
- ✅ App.tsx route + Layout.tsx nav — Task 4

**Placeholder scan:** None.

**Type consistency:**
- `middleware.PolicyRule{Name, Pattern, Action}` defined in Task 2, serialized via JSON in `PolicyRuleStore` — consistent.
- `services.PolicyRule` is the admin-layer struct (adds ID, IsActive, timestamps) — distinct from gateway's `middleware.PolicyRule`. No collision.
- `PolicyRulesServiceIface` method signatures match `PolicyRulesService` exactly.
