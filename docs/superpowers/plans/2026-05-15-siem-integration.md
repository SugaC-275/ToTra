# SIEM Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Export compliance events (PII violations, policy blocks) to enterprise SIEM systems via Push (webhook + retry queue) and Pull (REST API), with configurable event types and API Key authentication.

**Architecture:** Gateway middleware fires events into a buffered channel; a goroutine enqueuer reads from the channel and writes to `siem_delivery_queue` after matching active `siem_configs` by event type. An admin-side delivery worker polls the queue every 30s and POSTs to external endpoints with exponential back-off retry (3 attempts max). A Pull API returns a cursor-paginated unified event stream from three source tables.

**Tech Stack:** Go (Fiber, pgx/v5), AES-256-GCM encryption (existing `admin/crypto`), TypeScript/React (Tanstack Query)

---

## File Map

| Action | File |
|--------|------|
| Create | `infra/postgres/022_siem.sql` |
| Modify | `docker-compose.yml` |
| Create | `gateway/middleware/siem_event.go` |
| Create | `gateway/storage/siem_store.go` |
| Create | `gateway/storage/siem_store_test.go` |
| Modify | `gateway/middleware/pii.go` |
| Modify | `gateway/middleware/pii_test.go` |
| Modify | `gateway/middleware/policy.go` |
| Modify | `gateway/middleware/policy_test.go` |
| Modify | `gateway/main.go` |
| Create | `admin/services/siem.go` |
| Create | `admin/services/siem_test.go` |
| Create | `admin/services/siem_pull.go` |
| Create | `admin/services/siem_pull_test.go` |
| Create | `admin/api/siem.go` |
| Create | `admin/api/siem_test.go` |
| Modify | `admin/main.go` |
| Modify | `dashboard/src/api/client.ts` |
| Create | `dashboard/src/pages/admin/SIEMPage.tsx` |
| Modify | `dashboard/src/App.tsx` |
| Modify | `dashboard/src/components/Layout.tsx` |

---

### Task 1: DB Migration 022 — siem_configs + siem_delivery_queue

**Files:**
- Create: `infra/postgres/022_siem.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write the migration SQL**

Create `infra/postgres/022_siem.sql`:

```sql
CREATE TABLE siem_configs (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id        TEXT        NOT NULL,
  name             TEXT        NOT NULL,
  endpoint_url     TEXT        NOT NULL,
  api_key_encrypted TEXT       NOT NULL,
  event_types      TEXT[]      NOT NULL,
  is_active        BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON siem_configs (tenant_id);

CREATE TABLE siem_delivery_queue (
  id             BIGSERIAL   PRIMARY KEY,
  tenant_id      TEXT        NOT NULL,
  siem_config_id UUID        NOT NULL REFERENCES siem_configs(id) ON DELETE CASCADE,
  event_type     TEXT        NOT NULL,
  payload        JSONB       NOT NULL,
  status         TEXT        NOT NULL DEFAULT 'pending'
                             CHECK (status IN ('pending','delivered','failed')),
  attempts       INT         NOT NULL DEFAULT 0,
  next_retry_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at   TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON siem_delivery_queue (tenant_id, status, next_retry_at);
```

- [ ] **Step 2: Wire into docker-compose.yml**

In `docker-compose.yml`, inside the `postgres.volumes` list, add after the `021_budget_alert_log.sql` line:

```yaml
      - ./infra/postgres/022_siem.sql:/docker-entrypoint-initdb.d/022_siem.sql:ro
```

- [ ] **Step 3: Verify**

Run:
```bash
cd /Users/sugac.275/ToTra && docker compose up postgres -d --wait && docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "\dt siem*"
```
Expected: two rows — `siem_configs`, `siem_delivery_queue`.

- [ ] **Step 4: Commit**

```bash
git add infra/postgres/022_siem.sql docker-compose.yml
git commit -m "feat(db): migration 022 — siem_configs + siem_delivery_queue"
```

---

### Task 2: Gateway — SIEMEvent type + SIEMGatewayStore

**Files:**
- Create: `gateway/middleware/siem_event.go`
- Create: `gateway/storage/siem_store.go`
- Create: `gateway/storage/siem_store_test.go`

- [ ] **Step 1: Write the failing test**

Create `gateway/storage/siem_store_test.go`:

```go
package storage_test

import (
	"testing"

	"github.com/yourorg/totra/gateway/storage"
)

func TestSIEMGatewayStore_NewReturnsNonNil(t *testing.T) {
	s := storage.NewSIEMGatewayStore(nil)
	if s == nil {
		t.Fatal("expected non-nil SIEMGatewayStore")
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run TestSIEMGatewayStore -v
```
Expected: FAIL — `NewSIEMGatewayStore` undefined.

- [ ] **Step 3: Create SIEMEvent type**

Create `gateway/middleware/siem_event.go`:

```go
package middleware

// SIEMEvent is the event fired into the buffered channel when gateway middleware
// blocks a request. The Payload is the full push payload (source, tenant_id,
// event_type, occurred_at, detail) ready for delivery to external SIEM endpoints.
type SIEMEvent struct {
	TenantID  string
	EventType string
	Payload   map[string]any
}
```

- [ ] **Step 4: Create SIEMGatewayStore**

Create `gateway/storage/siem_store.go`:

```go
package storage

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SIEMConfigRow holds the columns the gateway needs to enqueue a delivery.
type SIEMConfigRow struct {
	ID              string
	EndpointURL     string
	APIKeyEncrypted string
}

type SIEMGatewayStore struct{ pool *pgxpool.Pool }

func NewSIEMGatewayStore(pool *pgxpool.Pool) *SIEMGatewayStore {
	return &SIEMGatewayStore{pool: pool}
}

// GetActiveConfigs returns all active siem_configs for the tenant that subscribe
// to the given event type.
func (s *SIEMGatewayStore) GetActiveConfigs(ctx context.Context, tenantID, eventType string) ([]*SIEMConfigRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, endpoint_url, api_key_encrypted
		 FROM siem_configs
		 WHERE tenant_id = $1 AND is_active = TRUE AND $2 = ANY(event_types)`,
		tenantID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*SIEMConfigRow
	for rows.Next() {
		var c SIEMConfigRow
		if err := rows.Scan(&c.ID, &c.EndpointURL, &c.APIKeyEncrypted); err != nil {
			return nil, err
		}
		configs = append(configs, &c)
	}
	return configs, rows.Err()
}

// EnqueueDelivery inserts one pending row into siem_delivery_queue.
func (s *SIEMGatewayStore) EnqueueDelivery(ctx context.Context, tenantID, configID, eventType string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO siem_delivery_queue (tenant_id, siem_config_id, event_type, payload)
		 VALUES ($1, $2, $3, $4)`,
		tenantID, configID, eventType, b)
	return err
}
```

- [ ] **Step 5: Run test — verify it passes**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run TestSIEMGatewayStore -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add gateway/middleware/siem_event.go gateway/storage/siem_store.go gateway/storage/siem_store_test.go
git commit -m "feat(gateway): SIEMEvent type + SIEMGatewayStore (GetActiveConfigs + EnqueueDelivery)"
```

---

### Task 3: PII Middleware — fire SIEM event on block

**Files:**
- Modify: `gateway/middleware/pii.go`
- Modify: `gateway/middleware/pii_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `gateway/middleware/pii_test.go` (after existing tests):

```go
func TestPIIMiddleware_SIEMChannelFired(t *testing.T) {
	ch := make(chan middleware.SIEMEvent, 1)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPIIMiddleware(nil, "", ch))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"messages":[{"content":"call 13800001234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)

	require.Len(t, ch, 1)
	ev := <-ch
	assert.Equal(t, "t1", ev.TenantID)
	assert.Equal(t, "pii_violation", ev.EventType)
}

func TestPIIMiddleware_SIEMChannelNilSafe(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "t1", nil))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"messages":[{"content":"call 13800001234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode) // must not panic with nil channel
}
```

Also update existing `setupPIIApp` to pass nil channel:
```go
func setupPIIApp() *fiber.App {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "test-tenant", nil)) // added nil
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestPIIMiddleware -v
```
Expected: FAIL — compile error (wrong number of args to NewPIIMiddleware) and missing test function.

- [ ] **Step 3: Update NewPIIMiddleware signature**

Replace the entire `NewPIIMiddleware` function in `gateway/middleware/pii.go`:

```go
// NewPIIMiddleware creates a PII detection middleware.
// recorder may be nil (no persistence). tenantID is a fallback when no
// authenticated user is present; pass "" to always pull from the "user" local.
// siemChan is optional — when non-nil, a SIEMEvent is sent (non-blocking) on every block.
func NewPIIMiddleware(recorder ViolationRecorder, tenantID string, siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
				tid := tenantID
				uid := ""
				if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
					tid = user.TenantID
					uid = user.UserID
				}
				if recorder != nil {
					recorder.RecordViolation(tid, uid, rule.name, "blocked", c.Path())
				}
				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  tid,
						EventType: "pii_violation",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   tid,
							"event_type":  "pii_violation",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":  uid,
								"pii_type": rule.name,
								"action":   "blocked",
								"path":     c.Path(),
							},
						},
					}:
					default: // channel full — drop without blocking the request
					}
				}
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential PII detected (" + rule.name + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}
```

Also add `"time"` to the import block in `pii.go`.

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestPIIMiddleware -v
```
Expected: all PASS.

- [ ] **Step 5: Verify the rest of gateway still compiles**

```bash
cd /Users/sugac.275/ToTra/gateway && go build ./...
```
Expected: compile error referencing `NewPIIMiddleware` in `gateway/main.go` (wrong arg count). Fix in Task 5.

- [ ] **Step 6: Commit**

```bash
git add gateway/middleware/pii.go gateway/middleware/pii_test.go
git commit -m "feat(gateway/pii): fire SIEMEvent to channel on PII block"
```

---

### Task 4: Policy Middleware — fire SIEM event on block

**Files:**
- Modify: `gateway/middleware/policy.go`
- Modify: `gateway/middleware/policy_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `gateway/middleware/policy_test.go` (after existing tests):

```go
func TestPolicyMiddleware_SIEMChannelFired(t *testing.T) {
	ch := make(chan middleware.SIEMEvent, 1)
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPolicyMiddleware(&stubPolicyStore{rules: rules}, ch))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"content":"SSN: 123-45-6789"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)

	require.Len(t, ch, 1)
	ev := <-ch
	assert.Equal(t, "t1", ev.TenantID)
	assert.Equal(t, "policy_block", ev.EventType)
}

func TestPolicyMiddleware_SIEMChannelNilSafe(t *testing.T) {
	rules := []*middleware.PolicyRule{
		{Name: "no-ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPolicyMiddleware(&stubPolicyStore{rules: rules}, nil))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"content":"SSN: 123-45-6789"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode) // must not panic with nil channel
}
```

Also update `setupPolicyApp` to pass nil channel:
```go
func setupPolicyApp(rules []*middleware.PolicyRule) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPolicyMiddleware(&stubPolicyStore{rules: rules}, nil)) // added nil
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	return app
}
```

Also add `require` to the import block in `policy_test.go`:
```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestPolicyMiddleware -v
```
Expected: FAIL — compile errors.

- [ ] **Step 3: Update NewPolicyMiddleware signature**

Replace the entire function in `gateway/middleware/policy.go`:

```go
// NewPolicyMiddleware checks request body against tenant policy rules and blocks
// on match. siemChan is optional — when non-nil, a SIEMEvent is sent (non-blocking)
// for every blocked request.
func NewPolicyMiddleware(store PolicyRuleGetter, siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}
		rules, err := store.GetRules(c.Context(), user.TenantID)
		if err != nil {
			log.Printf("policy middleware: %v", err)
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
					if siemChan != nil {
						select {
						case siemChan <- SIEMEvent{
							TenantID:  user.TenantID,
							EventType: "policy_block",
							Payload: map[string]any{
								"source":      "totra",
								"tenant_id":   user.TenantID,
								"event_type":  "policy_block",
								"occurred_at": time.Now().UTC().Format(time.RFC3339),
								"detail": map[string]any{
									"user_id":   user.UserID,
									"rule_name": rule.Name,
									"action":    "blocked",
									"path":      c.Path(),
								},
							},
						}:
						default:
						}
					}
					return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
						"error": fiber.Map{
							"message": "request blocked by policy rule: " + rule.Name,
							"type":    "policy_blocked",
						},
					})
				}
				log.Printf("policy rule matched (log): tenant=%s rule=%s", user.TenantID, rule.Name)
			}
		}
		return c.Next()
	}
}
```

Add `"time"` to the import block in `policy.go`.

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestPolicyMiddleware -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/policy.go gateway/middleware/policy_test.go
git commit -m "feat(gateway/policy): fire SIEMEvent to channel on policy block"
```

---

### Task 5: Gateway main.go — SIEMEnqueuer goroutine + wire middlewares

**Files:**
- Modify: `gateway/main.go`

- [ ] **Step 1: Add SIEMGatewayStore init + siemChan in main()**

In `gateway/main.go`, after `routingStore := storage.NewRoutingStore(pool)` (line ~48), add:

```go
siemGatewayStore := storage.NewSIEMGatewayStore(pool)
siemChan := make(chan middleware.SIEMEvent, 1000)
enqueuer := &siemEnqueuer{ch: siemChan, store: siemGatewayStore}
go enqueuer.run(context.Background())
```

- [ ] **Step 2: Update middleware calls to pass siemChan**

Replace the two middleware calls in the `v1` group (around line 64-65):

```go
// before:
middleware.NewPIIMiddleware(piiStore, ""),
middleware.NewPolicyMiddleware(policyRuleStore),

// after:
middleware.NewPIIMiddleware(piiStore, "", siemChan),
middleware.NewPolicyMiddleware(policyRuleStore, siemChan),
```

- [ ] **Step 3: Add siemEnqueuer struct at bottom of main.go**

Append to `gateway/main.go` (after `makeProxyHandler`):

```go
type siemEnqueuer struct {
	ch    <-chan middleware.SIEMEvent
	store *storage.SIEMGatewayStore
}

func (e *siemEnqueuer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-e.ch:
			configs, err := e.store.GetActiveConfigs(ctx, ev.TenantID, ev.EventType)
			if err != nil {
				log.Printf("siem enqueuer: get configs: %v", err)
				continue
			}
			for _, cfg := range configs {
				if err := e.store.EnqueueDelivery(ctx, ev.TenantID, cfg.ID, ev.EventType, ev.Payload); err != nil {
					log.Printf("siem enqueuer: enqueue config %s: %v", cfg.ID, err)
				}
			}
		}
	}
}
```

- [ ] **Step 4: Verify the gateway compiles**

```bash
cd /Users/sugac.275/ToTra/gateway && go build ./...
```
Expected: no errors.

- [ ] **Step 5: Run all gateway tests**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./...
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add gateway/main.go
git commit -m "feat(gateway): SIEMEnqueuer goroutine — channel → siem_delivery_queue"
```

---

### Task 6: Admin — SIEMConfigService + SIEMDeliveryService

**Files:**
- Create: `admin/services/siem.go`
- Create: `admin/services/siem_test.go`

- [ ] **Step 1: Write the failing tests**

Create `admin/services/siem_test.go`:

```go
package services_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

const testSIEMEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestSIEMConfigService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMConfigService(nil, testSIEMEncKey)
	assert.NotNil(t, svc)
}

func TestSIEMConfig_Fields(t *testing.T) {
	c := services.SIEMConfig{
		ID:          "abc-123",
		TenantID:    "acme",
		Name:        "Splunk",
		EndpointURL: "https://splunk.example.com/hec",
		EventTypes:  []string{"pii_violation", "policy_block"},
		IsActive:    true,
		CreatedAt:   time.Now(),
	}
	assert.Equal(t, "abc-123", c.ID)
	assert.Contains(t, c.EventTypes, "pii_violation")
	assert.True(t, c.IsActive)
}

func TestSIEMDeliveryService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMDeliveryService(nil, testSIEMEncKey)
	assert.NotNil(t, svc)
}

func TestDeliverToEndpoint_OK(t *testing.T) {
	var receivedAuth, receivedContentType string
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := services.DeliverToEndpoint(srv.URL, "my-api-key", map[string]any{
		"source":     "totra",
		"event_type": "pii_violation",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-api-key", receivedAuth)
	assert.Equal(t, "application/json", receivedContentType)
	assert.Contains(t, string(receivedBody), "pii_violation")
}

func TestDeliverToEndpoint_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := services.DeliverToEndpoint(srv.URL, "key", map[string]any{"event_type": "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestDeliverToEndpoint_BadURL(t *testing.T) {
	err := services.DeliverToEndpoint("http://127.0.0.1:0", "key", map[string]any{})
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestSIEMConfig|TestSIEMDelivery|TestDeliverToEndpoint" -v
```
Expected: FAIL — types not defined.

- [ ] **Step 3: Create admin/services/siem.go**

```go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

// ---- SIEMConfigService ----

type SIEMConfig struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	EndpointURL string    `json:"endpoint_url"`
	EventTypes  []string  `json:"event_types"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

type SIEMConfigService struct {
	pool   *pgxpool.Pool
	encKey string
}

func NewSIEMConfigService(pool *pgxpool.Pool, encKey string) *SIEMConfigService {
	return &SIEMConfigService{pool: pool, encKey: encKey}
}

func (s *SIEMConfigService) Create(ctx context.Context, tenantID, name, endpointURL, apiKey string, eventTypes []string) (*SIEMConfig, error) {
	encKey, err := crypto.Encrypt(apiKey, s.encKey)
	if err != nil {
		return nil, err
	}
	var c SIEMConfig
	err = s.pool.QueryRow(ctx,
		`INSERT INTO siem_configs (tenant_id, name, endpoint_url, api_key_encrypted, event_types)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, tenant_id, name, endpoint_url, event_types, is_active, created_at`,
		tenantID, name, endpointURL, encKey, eventTypes,
	).Scan(&c.ID, &c.TenantID, &c.Name, &c.EndpointURL, &c.EventTypes, &c.IsActive, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *SIEMConfigService) List(ctx context.Context, tenantID string) ([]*SIEMConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, endpoint_url, event_types, is_active, created_at
		 FROM siem_configs WHERE tenant_id = $1 ORDER BY created_at`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*SIEMConfig
	for rows.Next() {
		var c SIEMConfig
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.EndpointURL, &c.EventTypes, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if configs == nil {
		configs = []*SIEMConfig{}
	}
	return configs, nil
}

func (s *SIEMConfigService) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM siem_configs WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("siem config not found")
	}
	return nil
}

// ---- SIEMDeliveryService ----

type DeliveryLogRow struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}

type SIEMDeliveryService struct {
	pool   *pgxpool.Pool
	encKey string
}

func NewSIEMDeliveryService(pool *pgxpool.Pool, encKey string) *SIEMDeliveryService {
	return &SIEMDeliveryService{pool: pool, encKey: encKey}
}

// RunWorker polls siem_delivery_queue every 30s and delivers pending events.
// Call as a goroutine: go svc.RunWorker(ctx).
func (s *SIEMDeliveryService) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processQueue(ctx)
		}
	}
}

type deliveryRow struct {
	ID              int64
	TenantID        string
	ConfigID        string
	EventType       string
	Payload         map[string]any
	Attempts        int
	EndpointURL     string
	APIKeyEncrypted string
}

func (s *SIEMDeliveryService) processQueue(ctx context.Context) {
	rows, err := s.pool.Query(ctx,
		`SELECT q.id, q.tenant_id, q.siem_config_id, q.event_type, q.payload, q.attempts,
		        c.endpoint_url, c.api_key_encrypted
		 FROM siem_delivery_queue q
		 JOIN siem_configs c ON c.id = q.siem_config_id
		 WHERE q.status = 'pending' AND q.next_retry_at <= NOW()
		 ORDER BY q.created_at LIMIT 100`)
	if err != nil {
		log.Printf("siem worker: query: %v", err)
		return
	}
	defer rows.Close()

	var items []deliveryRow
	for rows.Next() {
		var d deliveryRow
		var payloadJSON []byte
		if err := rows.Scan(&d.ID, &d.TenantID, &d.ConfigID, &d.EventType, &payloadJSON, &d.Attempts, &d.EndpointURL, &d.APIKeyEncrypted); err != nil {
			log.Printf("siem worker: scan: %v", err)
			continue
		}
		if err := json.Unmarshal(payloadJSON, &d.Payload); err != nil {
			d.Payload = map[string]any{}
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		log.Printf("siem worker: rows: %v", err)
		return
	}

	for _, item := range items {
		apiKey, err := crypto.Decrypt(item.APIKeyEncrypted, s.encKey)
		if err != nil {
			log.Printf("siem worker: decrypt config %s: %v", item.ConfigID, err)
			continue
		}
		err = DeliverToEndpoint(item.EndpointURL, apiKey, item.Payload)
		if err == nil {
			s.pool.Exec(ctx,
				`UPDATE siem_delivery_queue SET status='delivered', delivered_at=NOW() WHERE id=$1`,
				item.ID)
		} else {
			newAttempts := item.Attempts + 1
			if newAttempts >= 3 {
				s.pool.Exec(ctx,
					`UPDATE siem_delivery_queue SET status='failed', attempts=$1 WHERE id=$2`,
					newAttempts, item.ID)
			} else {
				s.pool.Exec(ctx,
					`UPDATE siem_delivery_queue SET attempts=$1, next_retry_at=NOW()+($2*interval '1 minute') WHERE id=$3`,
					newAttempts, 1<<newAttempts, item.ID)
			}
		}
	}
}

// SendTest sends a test event to the specified SIEM config.
func (s *SIEMDeliveryService) SendTest(ctx context.Context, tenantID, configID string) error {
	var endpointURL, apiKeyEncrypted string
	err := s.pool.QueryRow(ctx,
		`SELECT endpoint_url, api_key_encrypted FROM siem_configs WHERE id=$1 AND tenant_id=$2`,
		configID, tenantID).Scan(&endpointURL, &apiKeyEncrypted)
	if err != nil {
		return fmt.Errorf("siem config not found")
	}
	apiKey, err := crypto.Decrypt(apiKeyEncrypted, s.encKey)
	if err != nil {
		return err
	}
	return DeliverToEndpoint(endpointURL, apiKey, map[string]any{
		"source":      "totra",
		"tenant_id":   tenantID,
		"event_type":  "test",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
		"detail":      map[string]any{"message": "ToTra SIEM integration test — connection successful"},
	})
}

// GetDeliveryLog returns the most recent delivery rows for a tenant.
func (s *SIEMDeliveryService) GetDeliveryLog(ctx context.Context, tenantID string, limit int) ([]*DeliveryLogRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, event_type, status, attempts, created_at
		 FROM siem_delivery_queue WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`,
		tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var log_ []*DeliveryLogRow
	for rows.Next() {
		var r DeliveryLogRow
		if err := rows.Scan(&r.ID, &r.EventType, &r.Status, &r.Attempts, &r.CreatedAt); err != nil {
			return nil, err
		}
		log_ = append(log_, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if log_ == nil {
		log_ = []*DeliveryLogRow{}
	}
	return log_, nil
}

// ---- HTTP delivery helper (exported for testing) ----

var siemHTTPClient = &http.Client{Timeout: 10 * time.Second}

// DeliverToEndpoint POSTs payload as JSON to endpointURL with Bearer auth.
func DeliverToEndpoint(endpointURL, apiKey string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpointURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := siemHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("SIEM endpoint returned %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestSIEMConfig|TestSIEMDelivery|TestDeliverToEndpoint" -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/siem.go admin/services/siem_test.go
git commit -m "feat(admin/services): SIEMConfigService + SIEMDeliveryService + DeliverToEndpoint"
```

---

### Task 7: Admin — SIEMPullService

**Files:**
- Create: `admin/services/siem_pull.go`
- Create: `admin/services/siem_pull_test.go`

- [ ] **Step 1: Write the failing tests**

Create `admin/services/siem_pull_test.go`:

```go
package services_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestSIEMPullService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMPullService(nil)
	assert.NotNil(t, svc)
}

func TestSIEMEvent_JSONFields(t *testing.T) {
	detail := json.RawMessage(`{"pii_type":"china_phone"}`)
	ev := services.SIEMEvent{
		ID:         "42",
		Type:       "pii_violation",
		TenantID:   "acme",
		OccurredAt: time.Now(),
		Detail:     detail,
	}
	b, err := json.Marshal(ev)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"pii_violation"`)
	assert.Contains(t, string(b), `"china_phone"`)
}

func TestSIEMEventsResult_NextSince(t *testing.T) {
	t0 := time.Now()
	res := services.SIEMEventsResult{
		Events:    []*services.SIEMEvent{},
		NextSince: t0,
	}
	assert.Equal(t, t0, res.NextSince)
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestSIEMPull -v
```
Expected: FAIL — `SIEMPullService` undefined.

- [ ] **Step 3: Create admin/services/siem_pull.go**

```go
package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SIEMEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TenantID   string          `json:"tenant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Detail     json.RawMessage `json:"detail"`
}

type SIEMEventsResult struct {
	Events    []*SIEMEvent `json:"events"`
	NextSince time.Time    `json:"next_since"`
}

type SIEMPullService struct{ pool *pgxpool.Pool }

func NewSIEMPullService(pool *pgxpool.Pool) *SIEMPullService {
	return &SIEMPullService{pool: pool}
}

// GetEvents returns a unified event stream from pii_violations, audit_log, and
// gateway_routing_events, filtered by tenant, since timestamp, event types,
// and page size. NextSince is the maximum occurred_at across returned events
// (use as the cursor for the next call).
func (s *SIEMPullService) GetEvents(ctx context.Context, tenantID string, since time.Time, types []string, limit int) (*SIEMEventsResult, error) {
	if len(types) == 0 {
		types = []string{"pii_violation", "policy_block", "audit_log", "quota_exceeded", "routing_event"}
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	const q = `
SELECT id::text, 'pii_violation' AS type, tenant_id::text, occurred_at,
       jsonb_build_object('user_id', user_id::text, 'pii_type', pii_type, 'action', action, 'path', request_path) AS detail
FROM pii_violations
WHERE tenant_id = $1::uuid AND occurred_at >= $2 AND 'pii_violation' = ANY($3::text[])

UNION ALL

SELECT id::text, 'audit_log', tenant_id, created_at,
       jsonb_build_object('record_type', record_type, 'record_id', record_id) AS detail
FROM audit_log
WHERE tenant_id = $1 AND created_at >= $2 AND 'audit_log' = ANY($3::text[])

UNION ALL

SELECT id::text, 'routing_event', tenant_id, routed_at,
       jsonb_build_object('original_model', original_model, 'routed_model', routed_model) AS detail
FROM gateway_routing_events
WHERE tenant_id = $1 AND routed_at >= $2 AND 'routing_event' = ANY($3::text[])

ORDER BY occurred_at DESC
LIMIT $4
`

	rows, err := s.pool.Query(ctx, q, tenantID, since, types, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &SIEMEventsResult{Events: []*SIEMEvent{}, NextSince: since}
	for rows.Next() {
		var ev SIEMEvent
		var detailJSON []byte
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.TenantID, &ev.OccurredAt, &detailJSON); err != nil {
			return nil, err
		}
		ev.Detail = json.RawMessage(detailJSON)
		result.Events = append(result.Events, &ev)
		if ev.OccurredAt.After(result.NextSince) {
			result.NextSince = ev.OccurredAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestSIEMPull -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/siem_pull.go admin/services/siem_pull_test.go
git commit -m "feat(admin/services): SIEMPullService — unified event pull from 3 tables"
```

---

### Task 8: Admin API — SIEM routes

**Files:**
- Create: `admin/api/siem.go`
- Create: `admin/api/siem_test.go`

- [ ] **Step 1: Write the failing tests**

Create `admin/api/siem_test.go`:

```go
package api_test

import (
	"bytes"
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

// ---- stubs ----

type stubSIEMCfgSvc struct {
	configs []*services.SIEMConfig
	delErr  error
}

func (s *stubSIEMCfgSvc) Create(_ context.Context, _, _, _, _ string, _ []string) (*services.SIEMConfig, error) {
	return &services.SIEMConfig{
		ID: "new-id", Name: "Splunk", EndpointURL: "https://splunk.example.com",
		EventTypes: []string{"pii_violation"}, IsActive: true, CreatedAt: time.Now(),
	}, nil
}
func (s *stubSIEMCfgSvc) List(_ context.Context, _ string) ([]*services.SIEMConfig, error) {
	return s.configs, nil
}
func (s *stubSIEMCfgSvc) Delete(_ context.Context, _, _ string) error { return s.delErr }

type stubSIEMDelivSvc struct{ testErr error }

func (s *stubSIEMDelivSvc) SendTest(_ context.Context, _, _ string) error { return s.testErr }
func (s *stubSIEMDelivSvc) GetDeliveryLog(_ context.Context, _ string, _ int) ([]*services.DeliveryLogRow, error) {
	return []*services.DeliveryLogRow{
		{ID: 1, EventType: "pii_violation", Status: "delivered", Attempts: 0, CreatedAt: time.Now()},
	}, nil
}

type stubSIEMPullSvc struct{}

func (s *stubSIEMPullSvc) GetEvents(_ context.Context, _ string, _ time.Time, _ []string, _ int) (*services.SIEMEventsResult, error) {
	return &services.SIEMEventsResult{Events: []*services.SIEMEvent{}, NextSince: time.Now()}, nil
}

func setupSIEMApp(cfgSvc *stubSIEMCfgSvc, delivSvc *stubSIEMDelivSvc, pullSvc *stubSIEMPullSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterSIEMRoutes(app, cfgSvc, delivSvc, pullSvc)
	return app
}

// ---- tests ----

func TestListSIEMConfigs_Empty(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCreateSIEMConfig_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	body := `{"name":"Splunk","endpoint_url":"https://splunk.example.com/hec","api_key":"secret","event_types":["pii_violation"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestCreateSIEMConfig_MissingFields(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	body := `{"name":"Splunk"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestDeleteSIEMConfig_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/siem/configs/some-id", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSendSIEMTest_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/siem/configs/some-id/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetSIEMEvents_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/events?limit=10", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetSIEMDeliveryLog_OK(t *testing.T) {
	app := setupSIEMApp(&stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/delivery-log", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSIEMRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "user"})
		return c.Next()
	})
	api.RegisterSIEMRoutes(app, &stubSIEMCfgSvc{}, &stubSIEMDelivSvc{}, &stubSIEMPullSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/siem/configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run TestSIEM -v
```
Expected: FAIL — `RegisterSIEMRoutes` undefined.

- [ ] **Step 3: Create admin/api/siem.go**

```go
package api

import (
	"context"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type SIEMConfigServiceIface interface {
	Create(ctx context.Context, tenantID, name, endpointURL, apiKey string, eventTypes []string) (*services.SIEMConfig, error)
	List(ctx context.Context, tenantID string) ([]*services.SIEMConfig, error)
	Delete(ctx context.Context, tenantID, id string) error
}

type SIEMDeliveryServiceIface interface {
	SendTest(ctx context.Context, tenantID, configID string) error
	GetDeliveryLog(ctx context.Context, tenantID string, limit int) ([]*services.DeliveryLogRow, error)
}

type SIEMPullServiceIface interface {
	GetEvents(ctx context.Context, tenantID string, since time.Time, types []string, limit int) (*services.SIEMEventsResult, error)
}

func RegisterSIEMRoutes(app fiber.Router, cfgSvc SIEMConfigServiceIface, delivSvc SIEMDeliveryServiceIface, pullSvc SIEMPullServiceIface) {
	app.Get("/api/admin/siem/configs", listSIEMConfigs(cfgSvc))
	app.Post("/api/admin/siem/configs", createSIEMConfig(cfgSvc))
	app.Delete("/api/admin/siem/configs/:id", deleteSIEMConfig(cfgSvc))
	app.Post("/api/admin/siem/configs/:id/test", sendSIEMTest(delivSvc))
	app.Get("/api/admin/siem/events", getSIEMEvents(pullSvc))
	app.Get("/api/admin/siem/delivery-log", getSIEMDeliveryLog(delivSvc))
}

func adminOnly(c *fiber.Ctx) (*services.Claims, bool) {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims.Role != "admin" {
		c.Status(403).JSON(fiber.Map{"error": "admin only"})
		return nil, false
	}
	return claims, true
}

func listSIEMConfigs(svc SIEMConfigServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		configs, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"configs": configs})
	}
}

func createSIEMConfig(svc SIEMConfigServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		var body struct {
			Name        string   `json:"name"`
			EndpointURL string   `json:"endpoint_url"`
			APIKey      string   `json:"api_key"`
			EventTypes  []string `json:"event_types"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Name == "" || body.EndpointURL == "" || body.APIKey == "" || len(body.EventTypes) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "name, endpoint_url, api_key, and event_types are required"})
		}
		cfg, err := svc.Create(c.Context(), claims.TenantID, body.Name, body.EndpointURL, body.APIKey, body.EventTypes)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(cfg)
	}
}

func deleteSIEMConfig(svc SIEMConfigServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		if err := svc.Delete(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

func sendSIEMTest(svc SIEMDeliveryServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		if err := svc.SendTest(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func getSIEMEvents(svc SIEMPullServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		since := time.Now().Add(-24 * time.Hour)
		if s := c.Query("since"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				since = t
			}
		}
		var types []string
		if t := c.Query("types"); t != "" {
			types = []string{t}
		}
		limit, _ := strconv.Atoi(c.Query("limit"))
		result, err := svc.GetEvents(c.Context(), claims.TenantID, since, types, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(result)
	}
}

func getSIEMDeliveryLog(svc SIEMDeliveryServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		limit, _ := strconv.Atoi(c.Query("limit"))
		if limit <= 0 {
			limit = 50
		}
		log_, err := svc.GetDeliveryLog(c.Context(), claims.TenantID, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"log": log_})
	}
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run TestSIEM -v
```
Expected: all PASS.

- [ ] **Step 5: Verify admin compiles**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```
Expected: no errors (main.go not yet wired, but package compiles).

- [ ] **Step 6: Commit**

```bash
git add admin/api/siem.go admin/api/siem_test.go
git commit -m "feat(admin/api): SIEM routes — list/create/delete configs + test/events/delivery-log"
```

---

### Task 9: Admin main.go — wire SIEM services + start delivery worker

**Files:**
- Modify: `admin/main.go`

- [ ] **Step 1: Add SIEM service instantiation**

In `admin/main.go`, after `policyRulesSvc := services.NewPolicyRulesService(pool)` (the last service created), add:

```go
siemCfgSvc := services.NewSIEMConfigService(pool, cfg.EncryptionKey)
siemDelivSvc := services.NewSIEMDeliveryService(pool, cfg.EncryptionKey)
siemPullSvc := services.NewSIEMPullService(pool)
go siemDelivSvc.RunWorker(context.Background())
```

Add `"context"` to the import block in `admin/main.go`.

- [ ] **Step 2: Register SIEM routes**

After `api.RegisterPolicyRulesRoutes(protected, policyRulesSvc)`, add:

```go
api.RegisterSIEMRoutes(protected, siemCfgSvc, siemDelivSvc, siemPullSvc)
```

- [ ] **Step 3: Verify admin compiles**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```
Expected: no errors.

- [ ] **Step 4: Run all admin tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./...
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/main.go
git commit -m "feat(admin): wire SIEM services + start delivery worker goroutine"
```

---

### Task 10: Dashboard — SIEM page

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/SIEMPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

- [ ] **Step 1: Add SIEM API functions to client.ts**

Append to `dashboard/src/api/client.ts`:

```typescript
// ---- SIEM ----

export interface SIEMConfig {
  id: string;
  tenant_id: string;
  name: string;
  endpoint_url: string;
  event_types: string[];
  is_active: boolean;
  created_at: string;
}

export interface DeliveryLogRow {
  id: number;
  event_type: string;
  status: string;
  attempts: number;
  created_at: string;
}

export const listSIEMConfigs = async (): Promise<{ configs: SIEMConfig[] }> => {
  const { data } = await apiClient.get("/api/admin/siem/configs");
  return data;
};

export const createSIEMConfig = async (payload: {
  name: string;
  endpoint_url: string;
  api_key: string;
  event_types: string[];
}): Promise<SIEMConfig> => {
  const { data } = await apiClient.post("/api/admin/siem/configs", payload);
  return data;
};

export const deleteSIEMConfig = async (id: string): Promise<void> => {
  await apiClient.delete(`/api/admin/siem/configs/${id}`);
};

export const sendSIEMTest = async (id: string): Promise<void> => {
  await apiClient.post(`/api/admin/siem/configs/${id}/test`);
};

export const getSIEMDeliveryLog = async (): Promise<{ log: DeliveryLogRow[] }> => {
  const { data } = await apiClient.get("/api/admin/siem/delivery-log?limit=50");
  return data;
};
```

- [ ] **Step 2: Create SIEMPage.tsx**

Create `dashboard/src/pages/admin/SIEMPage.tsx`:

```tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listSIEMConfigs,
  createSIEMConfig,
  deleteSIEMConfig,
  sendSIEMTest,
  getSIEMDeliveryLog,
} from "../../api/client";
import type { SIEMConfig, DeliveryLogRow } from "../../api/client";

const ALL_EVENT_TYPES = [
  "pii_violation",
  "policy_block",
  "audit_log",
  "quota_exceeded",
  "routing_event",
];

const STATUS_COLORS: Record<string, string> = {
  delivered: "text-green-600",
  pending: "text-yellow-600",
  failed: "text-red-600",
};

export default function SIEMPage() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [endpointURL, setEndpointURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [eventTypes, setEventTypes] = useState<string[]>([]);
  const [formError, setFormError] = useState("");

  const { data: cfgData, isLoading } = useQuery({
    queryKey: ["siem-configs"],
    queryFn: listSIEMConfigs,
  });

  const { data: logData } = useQuery({
    queryKey: ["siem-delivery-log"],
    queryFn: getSIEMDeliveryLog,
    refetchInterval: 30_000,
  });

  const createMutation = useMutation({
    mutationFn: createSIEMConfig,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["siem-configs"] });
      setName(""); setEndpointURL(""); setApiKey(""); setEventTypes([]); setFormError("");
    },
    onError: (err: any) => {
      setFormError(err?.response?.data?.error ?? "Failed to create config");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteSIEMConfig,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["siem-configs"] }),
  });

  const testMutation = useMutation({ mutationFn: sendSIEMTest });

  const toggleEventType = (et: string) =>
    setEventTypes((prev) =>
      prev.includes(et) ? prev.filter((x) => x !== et) : [...prev, et]
    );

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !endpointURL || !apiKey || eventTypes.length === 0) {
      setFormError("All fields are required");
      return;
    }
    createMutation.mutate({ name, endpoint_url: endpointURL, api_key: apiKey, event_types: eventTypes });
  };

  const configs: SIEMConfig[] = cfgData?.configs ?? [];
  const deliveryLog: DeliveryLogRow[] = logData?.log ?? [];

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">SIEM Integration</h1>

      {/* Config list */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Active Configurations</h2>
        {isLoading ? (
          <p className="text-gray-400 text-sm">Loading…</p>
        ) : configs.length === 0 ? (
          <p className="text-gray-400 text-sm">No SIEM configurations yet.</p>
        ) : (
          <ul className="divide-y">
            {configs.map((cfg) => (
              <li key={cfg.id} className="py-3 flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <p className="font-medium">{cfg.name}</p>
                  <p className="text-sm text-gray-500 truncate max-w-xs">{cfg.endpoint_url}</p>
                  <div className="flex gap-1 flex-wrap">
                    {cfg.event_types.map((et) => (
                      <span key={et} className="text-xs bg-blue-100 text-blue-700 rounded px-2 py-0.5">{et}</span>
                    ))}
                  </div>
                </div>
                <div className="flex gap-2 shrink-0">
                  <button
                    onClick={() => testMutation.mutate(cfg.id)}
                    disabled={testMutation.isPending}
                    className="text-sm px-3 py-1 border rounded hover:bg-gray-50"
                  >
                    Test
                  </button>
                  <button
                    onClick={() => deleteMutation.mutate(cfg.id)}
                    className="text-sm px-3 py-1 border border-red-300 text-red-600 rounded hover:bg-red-50"
                  >
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* New config form */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Add SIEM Configuration</h2>
        <form onSubmit={handleCreate} className="space-y-3">
          <input
            type="text" value={name} onChange={(e) => setName(e.target.value)}
            placeholder="Name (e.g. Splunk HEC)"
            className="block w-full border rounded px-3 py-2"
          />
          <input
            type="url" value={endpointURL} onChange={(e) => setEndpointURL(e.target.value)}
            placeholder="Endpoint URL"
            className="block w-full border rounded px-3 py-2"
          />
          <input
            type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
            placeholder="API Key"
            className="block w-full border rounded px-3 py-2"
          />
          <div className="space-y-1">
            <p className="text-sm font-medium text-gray-700">Event types to push:</p>
            <div className="flex flex-wrap gap-3">
              {ALL_EVENT_TYPES.map((et) => (
                <label key={et} className="flex items-center gap-1 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={eventTypes.includes(et)}
                    onChange={() => toggleEventType(et)}
                  />
                  {et}
                </label>
              ))}
            </div>
          </div>
          {formError && <p className="text-sm text-red-600">{formError}</p>}
          <button
            type="submit"
            disabled={createMutation.isPending}
            className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {createMutation.isPending ? "Saving…" : "Add Configuration"}
          </button>
        </form>
      </div>

      {/* Delivery log */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Push Delivery Log</h2>
        {deliveryLog.length === 0 ? (
          <p className="text-gray-400 text-sm">No delivery records yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-500 border-b">
                <th className="pb-2">Event Type</th>
                <th className="pb-2">Status</th>
                <th className="pb-2">Retries</th>
                <th className="pb-2">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {deliveryLog.map((row) => (
                <tr key={row.id}>
                  <td className="py-2">{row.event_type}</td>
                  <td className={`py-2 font-medium ${STATUS_COLORS[row.status] ?? ""}`}>{row.status}</td>
                  <td className="py-2">{row.attempts}</td>
                  <td className="py-2 text-gray-500">{new Date(row.created_at).toLocaleString()}</td>
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

- [ ] **Step 3: Add route in App.tsx**

In `dashboard/src/App.tsx`, add the import after the last page import:

```typescript
import SIEMPage from "./pages/admin/SIEMPage";
```

Add the route after the `admin/cost` route:

```tsx
<Route path="admin/siem" element={<ProtectedRoute adminOnly><SIEMPage /></ProtectedRoute>} />
```

- [ ] **Step 4: Add sidebar link in Layout.tsx**

In `dashboard/src/components/Layout.tsx`, find the `adminLinks` array (or equivalent nav items) and add after the bot-configs entry:

```typescript
{ label: "SIEM Integration", href: "/admin/siem" },
```

- [ ] **Step 5: Run TypeScript type-check**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run type-check 2>&1 | tail -20
```
If `type-check` script doesn't exist:
```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1 | tail -20
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/api/client.ts dashboard/src/pages/admin/SIEMPage.tsx dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "feat(dashboard): SIEM integration page — config list, new form, delivery log"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** Migration 022 ✓ | Gateway channel + enqueuer ✓ | PII/Policy middleware SIEM events ✓ | SIEMConfigService (Create/List/Delete) ✓ | SIEMDeliveryService (RunWorker 30s, retry 2^n minutes, 3 attempts → failed, SendTest, GetDeliveryLog) ✓ | SIEMPullService (union 3 tables, since cursor) ✓ | 6 API endpoints ✓ | Dashboard (config list, form, delivery log) ✓
- [x] **Type consistency:** `SIEMConfig`, `DeliveryLogRow`, `SIEMEvent`, `SIEMEventsResult` defined once and referenced consistently across Tasks 6–9 | `SIEMConfigServiceIface`, `SIEMDeliveryServiceIface`, `SIEMPullServiceIface` in `admin/api/siem.go` match concrete service method signatures
- [x] **No placeholders:** All steps contain complete code
- [x] **Retry formula:** `1<<newAttempts` minutes = 2 min → 4 min → (fail at 3 attempts)
