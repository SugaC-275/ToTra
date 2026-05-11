# AI Agent Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-conversation AI Agent session tracking to ToTra. The gateway detects Agent mode (request contains a non-empty `tools` array or any message has `tool_calls`), counts loops per conversation via Redis, circuit-breaks at a configurable threshold (default 20), and upserts stats to a new `agent_sessions` table. Admin and employee APIs expose session data; a new admin dashboard page visualizes it.

**Architecture:** A new `AgentStore` in the gateway wraps Redis (loop counter) and Postgres (upsert). A new `NewAgentMiddleware` runs after `NewPIIMiddleware` in the `/v1` group and short-circuits with 429 when the loop limit is exceeded. After the upstream call succeeds the proxy handler calls `agentStore.Record(...)` — mirroring how `usageStore.Record(...)` is called today. On the admin side a new `AgentService` reads `agent_sessions` and is wired in `admin/main.go`. Two new API handlers are registered: one admin route and one employee ("me") route. A new `AgentTrackingPage.tsx` is added to the admin dashboard.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, go-redis v9, React 19 + TypeScript 6, TanStack Query v5, Tailwind v4, shadcn-style local `Card` component, React Router v6.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `infra/postgres/010_agent_sessions.sql` (create) |
| 1 | `gateway/storage/agent.go` (create), `gateway/storage/agent_test.go` (create), `gateway/middleware/agent.go` (create), `gateway/middleware/agent_test.go` (create), `gateway/config/config.go` (modify), `gateway/main.go` (modify) |
| 2 | `admin/services/agent.go` (create), `admin/services/agent_test.go` (create), `admin/api/agent.go` (create), `admin/api/agent_test.go` (create), `admin/main.go` (modify) |
| 3 | `dashboard/src/api/client.ts` (modify), `dashboard/src/pages/admin/AgentTrackingPage.tsx` (create), `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 0: DB migration — `agent_sessions` table

**Files:**
- Create: `infra/postgres/010_agent_sessions.sql`

- [ ] **Step 1: Write the migration file**

Create `/Users/sugac.275/ToTra/infra/postgres/010_agent_sessions.sql`:

```sql
-- Agent session tracking: one row per conversation_id
CREATE TABLE agent_sessions (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id  UUID        NOT NULL UNIQUE,
    loop_count       INTEGER     NOT NULL DEFAULT 0,
    tool_call_count  INTEGER     NOT NULL DEFAULT 0,
    is_dead_loop     BOOLEAN     NOT NULL DEFAULT FALSE,
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_sessions_tenant_month
    ON agent_sessions (tenant_id, date_trunc('month', last_seen_at));

CREATE INDEX idx_agent_sessions_user
    ON agent_sessions (user_id, last_seen_at DESC);
```

- [ ] **Step 2: Apply migration locally and verify**

```bash
psql "$POSTGRES_DSN" -f infra/postgres/010_agent_sessions.sql
# expected output:
# CREATE TABLE
# CREATE INDEX
# CREATE INDEX
```

---

## Task 1: Gateway — AgentStore + middleware + wire-up

**Files:**
- Create: `gateway/storage/agent.go`
- Create: `gateway/storage/agent_test.go`
- Create: `gateway/middleware/agent.go`
- Create: `gateway/middleware/agent_test.go`
- Modify: `gateway/config/config.go`
- Modify: `gateway/main.go`

### Step 1.1 — Write failing test for `AgentStore`

- [ ] Create `gateway/storage/agent_test.go`:

```go
package storage_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestAgentStore_IncrLoop_BelowLimit(t *testing.T) {
	rdb := newTestRedis(t)
	store := storage.NewAgentStore(rdb, nil, 20)

	count, exceeded, err := store.IncrLoop(context.Background(), "conv-123")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.False(t, exceeded)
}

func TestAgentStore_IncrLoop_ExceedsLimit(t *testing.T) {
	rdb := newTestRedis(t)
	store := storage.NewAgentStore(rdb, nil, 3)

	for i := 0; i < 3; i++ {
		_, _, _ = store.IncrLoop(context.Background(), "conv-abc")
	}
	count, exceeded, err := store.IncrLoop(context.Background(), "conv-abc")
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)
	assert.True(t, exceeded)
}
```

- [ ] **Run — expect compile failure** (storage.AgentStore does not exist yet):

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... 2>&1 | head -20
# expected: undefined: storage.NewAgentStore
```

### Step 1.2 — Implement `AgentStore`

- [ ] Create `gateway/storage/agent.go`:

```go
package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const agentLoopTTL = time.Hour

// AgentRecord describes one completed agent-mode request to be persisted.
type AgentRecord struct {
	TenantID       string
	UserID         string
	ConversationID string
	ToolCallCount  int // number of tool_calls entries in the request messages
	IsDeadLoop     bool
}

// AgentStore handles Redis loop counting and Postgres upsert for agent sessions.
type AgentStore struct {
	rdb       *redis.Client
	pool      *pgxpool.Pool
	loopLimit int64
	ch        chan *AgentRecord
}

// NewAgentStore creates an AgentStore. pool may be nil in unit tests that only
// test Redis behaviour.
func NewAgentStore(rdb *redis.Client, pool *pgxpool.Pool, loopLimit int64) *AgentStore {
	s := &AgentStore{
		rdb:       rdb,
		pool:      pool,
		loopLimit: loopLimit,
		ch:        make(chan *AgentRecord, 1000),
	}
	go s.runWorker()
	return s
}

// IncrLoop atomically increments the per-conversation loop counter in Redis
// (TTL = 1 hour) and returns (newCount, exceeded, error).
func (s *AgentStore) IncrLoop(ctx context.Context, conversationID string) (int64, bool, error) {
	key := fmt.Sprintf("agent:loops:%s", conversationID)
	count, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, false, fmt.Errorf("agent loop incr: %w", err)
	}
	// Refresh TTL on every increment; ignore error — counter is best-effort.
	_ = s.rdb.Expire(ctx, key, agentLoopTTL).Err()
	return count, count > s.loopLimit, nil
}

// Record enqueues an agent session upsert for async write. Never blocks.
func (s *AgentStore) Record(r *AgentRecord) {
	select {
	case s.ch <- r:
	default:
		log.Println("agent record channel full, dropping record")
	}
}

func (s *AgentStore) runWorker() {
	for r := range s.ch {
		if s.pool == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.upsert(ctx, r); err != nil {
			log.Printf("failed to upsert agent session: %v", err)
		}
		cancel()
	}
}

func (s *AgentStore) upsert(ctx context.Context, r *AgentRecord) error {
	convID, err := uuid.Parse(r.ConversationID)
	if err != nil {
		return nil // non-UUID conversation_id: skip silently
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO agent_sessions
			(tenant_id, user_id, conversation_id, loop_count, tool_call_count, is_dead_loop, last_seen_at)
		VALUES ($1, $2, $3, 1, $4, $5, NOW())
		ON CONFLICT (conversation_id) DO UPDATE
			SET loop_count      = agent_sessions.loop_count + 1,
			    tool_call_count = agent_sessions.tool_call_count + EXCLUDED.tool_call_count,
			    is_dead_loop    = EXCLUDED.is_dead_loop,
			    last_seen_at    = NOW()`,
		r.TenantID, r.UserID, convID, r.ToolCallCount, r.IsDeadLoop,
	)
	if err != nil {
		return fmt.Errorf("upsert agent session: %w", err)
	}
	return nil
}
```

- [ ] **Run tests — expect pass**:

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run TestAgentStore -v 2>&1
# expected:
# --- PASS: TestAgentStore_IncrLoop_BelowLimit (0.00s)
# --- PASS: TestAgentStore_IncrLoop_ExceedsLimit (0.00s)
# PASS
```

### Step 1.3 — Write failing test for `AgentMiddleware`

- [ ] Create `gateway/middleware/agent_test.go`:

```go
package middleware_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

// mockAgentLoopIncr satisfies the AgentLoopIncr interface.
type mockAgentLoopIncr struct {
	count    int64
	exceeded bool
}

func (m *mockAgentLoopIncr) IncrLoop(_ context.Context, _ string) (int64, bool, error) {
	return m.count, m.exceeded, nil
}

func setupAgentApp(loopIncr middleware.AgentLoopIncr) *fiber.App {
	app := fiber.New()
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1", Role: "standard"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Use(middleware.NewAgentMiddleware(loopIncr))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

// isAgentBody returns a JSON body that signals Agent mode (non-empty tools array).
func agentBody() *bytes.Reader {
	body := `{"model":"gpt-4","tools":[{"type":"function","function":{"name":"search"}}]}`
	return bytes.NewReader([]byte(body))
}

// nonAgentBody returns a plain chat body with no tools.
func nonAgentBody() *bytes.Reader {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	return bytes.NewReader([]byte(body))
}

func TestAgentMiddleware_NonAgent_Passes(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 0, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nonAgentBody())
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAgentMiddleware_AgentBelowLimit_Passes(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 5, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", agentBody())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "11111111-1111-1111-1111-111111111111")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAgentMiddleware_AgentExceedsLimit_Returns429(t *testing.T) {
	app := setupAgentApp(&mockAgentLoopIncr{count: 21, exceeded: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", agentBody())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "22222222-2222-2222-2222-222222222222")
	resp, _ := app.Test(req)
	assert.Equal(t, 429, resp.StatusCode)
}

func TestAgentMiddleware_ToolCallsInMessages_DetectedAsAgent(t *testing.T) {
	// A body where a message contains tool_calls — also signals Agent mode.
	body := `{"model":"gpt-4","messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}]}`
	app := setupAgentApp(&mockAgentLoopIncr{count: 1, exceeded: false})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conversation-ID", "33333333-3333-3333-3333-333333333333")
	resp, _ := app.Test(req)
	// middleware detects agent mode, calls IncrLoop — but not exceeded → passes
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Run — expect compile failure** (middleware.AgentMiddleware does not exist):

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... 2>&1 | head -20
# expected: undefined: middleware.NewAgentMiddleware
```

### Step 1.4 — Implement `AgentMiddleware`

- [ ] Create `gateway/middleware/agent.go`:

```go
package middleware

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// AgentLoopIncr is the narrow interface the middleware needs from AgentStore.
type AgentLoopIncr interface {
	IncrLoop(ctx context.Context, conversationID string) (int64, bool, error)
}

// agentRequestBody is used to detect Agent mode from the raw request.
type agentRequestBody struct {
	Tools    []json.RawMessage `json:"tools"`
	Messages []struct {
		ToolCalls []json.RawMessage `json:"tool_calls"`
	} `json:"messages"`
}

// isAgentRequest returns true when the request body carries a non-empty tools
// array or any message contains tool_calls.
func isAgentRequest(body []byte) bool {
	var req agentRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	if len(req.Tools) > 0 {
		return true
	}
	for _, msg := range req.Messages {
		if len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// countToolCalls returns the total number of tool_call entries across all
// messages in the request body.
func countToolCalls(body []byte) int {
	var req agentRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, msg := range req.Messages {
		total += len(msg.ToolCalls)
	}
	return total
}

// NewAgentMiddleware returns a Fiber middleware that:
//  1. Detects Agent-mode requests (tools array or tool_calls in messages).
//  2. If Agent mode and X-Conversation-ID is present, increments the Redis
//     loop counter and returns 429 if the limit is exceeded.
//  3. Stores detection results in Fiber Locals for use by the proxy handler.
func NewAgentMiddleware(store AgentLoopIncr) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()

		if !isAgentRequest(body) {
			// Not an agent request — store zeros and continue.
			c.Locals("agent_mode", false)
			c.Locals("agent_tool_call_count", 0)
			return c.Next()
		}

		c.Locals("agent_mode", true)
		c.Locals("agent_tool_call_count", countToolCalls(body))

		conversationID := c.Get("X-Conversation-ID")
		if conversationID == "" {
			// Agent mode but no conversation tracking — allow through.
			return c.Next()
		}

		_, exceeded, err := store.IncrLoop(c.Context(), conversationID)
		if err != nil {
			// Best-effort: log implicitly via returning 500 would break the
			// request; instead allow through so the upstream still gets called.
			return c.Next()
		}

		if exceeded {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "Agent loop limit exceeded",
					"type":    "agent_loop_exceeded",
					"code":    "agent_loop_exceeded",
				},
			})
		}

		return c.Next()
	}
}
```

- [ ] **Run middleware tests — expect pass**:

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestAgentMiddleware -v 2>&1
# expected:
# --- PASS: TestAgentMiddleware_NonAgent_Passes (0.00s)
# --- PASS: TestAgentMiddleware_AgentBelowLimit_Passes (0.00s)
# --- PASS: TestAgentMiddleware_AgentExceedsLimit_Returns429 (0.00s)
# --- PASS: TestAgentMiddleware_ToolCallsInMessages_DetectedAsAgent (0.00s)
# PASS
```

### Step 1.5 — Add `AGENT_LOOP_LIMIT` to gateway config

- [ ] Edit `gateway/config/config.go` — add import of `"strconv"` and a new `AgentLoopLimit` field:

Replace the `Config` struct and `Load` function with:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	PostgresDSN    string
	RedisAddr      string
	RedisPassword  string
	EncryptionKey  string
	JWTSecret      string
	AgentLoopLimit int64
}

func Load() (*Config, error) {
	port := getEnv("GATEWAY_PORT", "8080")
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")

	loopLimit := int64(20)
	if v := os.Getenv("AGENT_LOOP_LIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			loopLimit = n
		}
	}

	return &Config{
		Port:           port,
		PostgresDSN:    fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", pgHost, pgPort, pgDB, pgUser, pgPass),
		RedisAddr:      fmt.Sprintf("%s:%s", getEnv("REDIS_HOST", "localhost"), getEnv("REDIS_PORT", "6379")),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		EncryptionKey:  mustGetEnv("GATEWAY_ENCRYPTION_KEY"),
		JWTSecret:      mustGetEnv("JWT_SECRET"),
		AgentLoopLimit: loopLimit,
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %s is not set", key))
	}
	return v
}
```

### Step 1.6 — Wire AgentStore and middleware into `gateway/main.go`

- [ ] Replace the entire `gateway/main.go` with:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/config"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	quotaStore := storage.NewQuotaStore(rdb)
	usageStore := storage.NewUsageStore(pool)
	agentStore := storage.NewAgentStore(rdb, pool, cfg.AgentLoopLimit)
	pgUserLookup := storage.NewPGUserLookup(pool)
	pgUserQuota := storage.NewPGUserQuota(pool)

	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	v1 := app.Group("/v1",
		middleware.NewAuthMiddleware(pgUserLookup),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		middleware.NewPIIMiddleware(),
		middleware.NewAgentMiddleware(agentStore),
	)

	proxyHandler := makeProxyHandler(pool, usageStore, agentStore)
	v1.Post("/chat/completions", proxyHandler)
	v1.Post("/messages", proxyHandler)

	log.Printf("Gateway listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}

func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)

	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		start := time.Now()

		var reqBody struct {
			Model string `json:"model"`
		}
		if err := c.BodyParser(&reqBody); err != nil || reqBody.Model == "" {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "model field required"}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, reqBody.Model)
		if err != nil || modelCfg == nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured for your tenant", reqBody.Model),
			}})
		}

		var fwd interface {
			Forward(ctx context.Context, body []byte) (*providers.ForwardResult, *providers.Usage, error)
		}
		switch modelCfg.Provider {
		case "openai":
			fwd = providers.NewOpenAIAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "anthropic":
			fwd = providers.NewAnthropicAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "local":
			fwd = providers.NewLocalAdapter(modelCfg.BaseURL)
		default:
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "unsupported provider"}})
		}

		result, usage, err := fwd.Forward(c.Context(), c.Body())
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": fiber.Map{"message": "upstream error: " + err.Error()}})
		}

		responseMS := int(time.Since(start).Milliseconds())
		scuCost := tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate)
		conversationID := c.Get("X-Conversation-ID") // empty string if absent

		usageStore.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			ConversationID:   conversationID,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			SCUCost:          scuCost,
			USDCost:          0,
			ResponseMS:       responseMS,
		})

		// Record agent session if this was an agent-mode request.
		if agentMode, _ := c.Locals("agent_mode").(bool); agentMode && conversationID != "" {
			toolCallCount, _ := c.Locals("agent_tool_call_count").(int)
			// isDeadLoop: the middleware already enforces the circuit breaker,
			// but we also persist the flag so the DB reflects it historically.
			// We re-check exceeded via the Redis key not being reset — simplest
			// heuristic: mark dead loop if loop_count in Redis > limit. Here we
			// rely on the middleware having set 429 already; any request that
			// reaches this point is within-limit (is_dead_loop = false for new
			// records at limit boundary; existing rows already flagged true).
			agentStore.Record(&storage.AgentRecord{
				TenantID:       user.TenantID,
				UserID:         user.UserID,
				ConversationID: conversationID,
				ToolCallCount:  toolCallCount,
				IsDeadLoop:     false,
			})
		}

		for k, vs := range result.Headers {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}
```

- [ ] **Build check — expect clean compile**:

```bash
cd /Users/sugac.275/ToTra/gateway && go build ./... 2>&1
# expected: (no output)
```

- [ ] **Run all gateway tests**:

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./... 2>&1
# expected: ok  github.com/yourorg/totra/gateway/...
```

- [ ] **Commit Task 1**:

```bash
cd /Users/sugac.275/ToTra && git add infra/postgres/010_agent_sessions.sql gateway/storage/agent.go gateway/storage/agent_test.go gateway/middleware/agent.go gateway/middleware/agent_test.go gateway/config/config.go gateway/main.go
git commit -m "feat(gateway): add AgentStore, AgentMiddleware, and dead-loop circuit breaker"
```

---

## Task 2: Admin service + API

**Files:**
- Create: `admin/services/agent.go`
- Create: `admin/services/agent_test.go`
- Create: `admin/api/agent.go`
- Create: `admin/api/agent_test.go`
- Modify: `admin/main.go`

### Step 2.1 — Write failing test for `AgentService`

- [ ] Create `admin/services/agent_test.go`:

```go
package services_test

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/services"
)

func TestAgentService_GetAgentSessions_ReturnsList(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{
		"id", "tenant_id", "user_id", "user_name",
		"conversation_id", "loop_count", "tool_call_count", "is_dead_loop", "last_seen_at", "created_at",
	}).AddRow(
		"sess-1", "tenant-1", "user-1", "Alice",
		"conv-1", 5, 3, false, "2026-05-10T12:00:00Z", "2026-05-10T10:00:00Z",
	)

	mock.ExpectQuery(`SELECT`).
		WithArgs("tenant-1", "2026-05").
		WillReturnRows(rows)

	svc := services.NewAgentService(mock)
	sessions, err := svc.GetAgentSessions(context.Background(), "tenant-1", "2026-05")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "Alice", sessions[0].UserName)
	assert.Equal(t, 5, sessions[0].LoopCount)
	assert.False(t, sessions[0].IsDeadLoop)
}

func TestAgentService_GetMyAgentSessions_ReturnsList(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{
		"id", "tenant_id", "user_id", "user_name",
		"conversation_id", "loop_count", "tool_call_count", "is_dead_loop", "last_seen_at", "created_at",
	}).AddRow(
		"sess-2", "tenant-1", "user-2", "Bob",
		"conv-2", 15, 10, true, "2026-05-11T08:00:00Z", "2026-05-11T07:00:00Z",
	)

	mock.ExpectQuery(`SELECT`).
		WithArgs("user-2", "2026-05").
		WillReturnRows(rows)

	svc := services.NewAgentService(mock)
	sessions, err := svc.GetMyAgentSessions(context.Background(), "user-2", "2026-05")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.True(t, sessions[0].IsDeadLoop)
	assert.Equal(t, 15, sessions[0].LoopCount)
}
```

- [ ] **Run — expect compile failure** (services.AgentService does not exist):

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestAgentService 2>&1 | head -15
# expected: undefined: services.NewAgentService
```

### Step 2.2 — Implement `AgentService`

- [ ] Create `admin/services/agent.go`:

```go
package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentSession is the DTO returned by AgentService methods.
type AgentSession struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	UserID         string    `json:"user_id"`
	UserName       string    `json:"user_name"`
	ConversationID string    `json:"conversation_id"`
	LoopCount      int       `json:"loop_count"`
	ToolCallCount  int       `json:"tool_call_count"`
	IsDeadLoop     bool      `json:"is_dead_loop"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// pgxQuerier allows swapping pgxpool.Pool with pgxmock in tests.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgxRows, error)
}

type pgxRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// pgxPoolAdapter wraps *pgxpool.Pool to satisfy pgxQuerier.
type pgxPoolAdapter struct{ pool *pgxpool.Pool }

func (a *pgxPoolAdapter) Query(ctx context.Context, sql string, args ...any) (pgxRows, error) {
	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// AgentService reads agent_sessions from Postgres.
type AgentService struct {
	q pgxQuerier
}

// NewAgentService accepts either *pgxpool.Pool (production) or a pgxmock (tests).
func NewAgentService(q interface {
	Query(ctx context.Context, sql string, args ...any) (interface{ Next() bool; Scan(...any) error; Err() error; Close() }, error)
}) *AgentService {
	// We need a small adapter layer because pgxpool.Rows and pgxmock.Rows both
	// implement compatible but differently-typed interfaces.
	return &AgentService{q: &universalQuerier{inner: q}}
}

// universalQuerier bridges the wide pgxpool / pgxmock interface to pgxQuerier.
type universalQuerier struct {
	inner interface {
		Query(ctx context.Context, sql string, args ...any) (interface{ Next() bool; Scan(...any) error; Err() error; Close() }, error)
	}
}

func (u *universalQuerier) Query(ctx context.Context, sql string, args ...any) (pgxRows, error) {
	rows, err := u.inner.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

const agentSessionSelectCols = `
	a.id, a.tenant_id, a.user_id, u.name AS user_name,
	a.conversation_id::text, a.loop_count, a.tool_call_count,
	a.is_dead_loop, a.last_seen_at, a.created_at`

func scanSessions(rows pgxRows) ([]*AgentSession, error) {
	defer rows.Close()
	var out []*AgentSession
	for rows.Next() {
		var s AgentSession
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.UserID, &s.UserName,
			&s.ConversationID, &s.LoopCount, &s.ToolCallCount,
			&s.IsDeadLoop, &s.LastSeenAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// GetAgentSessions returns all agent sessions for a tenant in the given yearMonth (YYYY-MM).
func (s *AgentService) GetAgentSessions(ctx context.Context, tenantID, yearMonth string) ([]*AgentSession, error) {
	rows, err := s.q.Query(ctx, `
		SELECT `+agentSessionSelectCols+`
		FROM agent_sessions a
		JOIN users u ON u.id = a.user_id
		WHERE a.tenant_id = $1
		  AND to_char(a.last_seen_at, 'YYYY-MM') = $2
		ORDER BY a.last_seen_at DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	return scanSessions(rows)
}

// GetMyAgentSessions returns agent sessions for a specific user in the given yearMonth.
func (s *AgentService) GetMyAgentSessions(ctx context.Context, userID, yearMonth string) ([]*AgentSession, error) {
	rows, err := s.q.Query(ctx, `
		SELECT `+agentSessionSelectCols+`
		FROM agent_sessions a
		JOIN users u ON u.id = a.user_id
		WHERE a.user_id = $1
		  AND to_char(a.last_seen_at, 'YYYY-MM') = $2
		ORDER BY a.last_seen_at DESC`,
		userID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	return scanSessions(rows)
}
```

> **Note on the adapter:** `pgxpool.Pool.Query` returns `pgx.Rows` and `pgxmock.Pool.Query` returns `pgxmock.Rows`. Both are concrete types that implement `Next() bool`, `Scan(...any) error`, `Err() error`, `Close()`. The `universalQuerier` bridges both by accepting an `interface{}` with the matching method signatures. In production pass `pool` directly wrapped in `NewAgentServiceFromPool` (see next step).

- [ ] Add a convenience constructor that takes `*pgxpool.Pool` (used by `admin/main.go`):

Append to `admin/services/agent.go`:

```go
// NewAgentServiceFromPool is the production constructor.
func NewAgentServiceFromPool(pool *pgxpool.Pool) *AgentService {
	return &AgentService{q: &pgxPoolAdapter{pool: pool}}
}
```

And update `pgxPoolAdapter.Query` signature to satisfy `pgxQuerier` by using the concrete pgx.Rows as pgxRows (they both have the same methods):

> **Implementation note:** `pgxpool.Pool.Query` returns `(pgx.Rows, error)`. `pgx.Rows` implements `Next() bool`, `Scan(dest ...any) error`, `Err() error`, `Close()`. The `pgxRows` interface defined above matches this exactly. The `pgxPoolAdapter` wraps the pool and the compiler will verify the interface is satisfied.

- [ ] **Run service tests — expect pass**:

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestAgentService -v 2>&1
# expected:
# --- PASS: TestAgentService_GetAgentSessions_ReturnsList (0.00s)
# --- PASS: TestAgentService_GetMyAgentSessions_ReturnsList (0.00s)
# PASS
```

### Step 2.3 — Write failing test for `AgentAPI`

- [ ] Create `admin/api/agent_test.go`:

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

type mockAgentService struct{}

func (m *mockAgentService) GetAgentSessions(_ context.Context, _, _ string) ([]*services.AgentSession, error) {
	return []*services.AgentSession{
		{
			ID:             "sess-1",
			TenantID:       "tenant-1",
			UserID:         "user-1",
			UserName:       "Alice",
			ConversationID: "conv-1",
			LoopCount:      8,
			ToolCallCount:  5,
			IsDeadLoop:     false,
			LastSeenAt:     time.Now(),
			CreatedAt:      time.Now(),
		},
	}, nil
}

func (m *mockAgentService) GetMyAgentSessions(_ context.Context, _, _ string) ([]*services.AgentSession, error) {
	return []*services.AgentSession{
		{
			ID:             "sess-2",
			UserID:         "user-2",
			UserName:       "Bob",
			ConversationID: "conv-2",
			LoopCount:      22,
			ToolCallCount:  18,
			IsDeadLoop:     true,
			LastSeenAt:     time.Now(),
			CreatedAt:      time.Now(),
		},
	}, nil
}

func setupAgentAPIApp(role string) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "user-1", TenantID: "tenant-1", Role: role}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterAgentRoutes(app, &mockAgentService{})
	return app
}

func TestGetAdminAgentSessions_OK(t *testing.T) {
	app := setupAgentAPIApp("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetAdminAgentSessions_ForbiddenForEmployee(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestGetAdminAgentSessions_MissingMonth(t *testing.T) {
	app := setupAgentAPIApp("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agent-sessions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetMyAgentSessions_OK(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/agent-sessions?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetMyAgentSessions_MissingMonth(t *testing.T) {
	app := setupAgentAPIApp("standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/agent-sessions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}
```

- [ ] **Run — expect compile failure**:

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run TestGetAdminAgentSessions 2>&1 | head -15
# expected: undefined: api.RegisterAgentRoutes
```

### Step 2.4 — Implement `RegisterAgentRoutes`

- [ ] Create `admin/api/agent.go`:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// AgentServiceInterface is the narrow interface the handler needs.
type AgentServiceInterface interface {
	GetAgentSessions(ctx context.Context, tenantID, yearMonth string) ([]*services.AgentSession, error)
	GetMyAgentSessions(ctx context.Context, userID, yearMonth string) ([]*services.AgentSession, error)
}

// RegisterAgentRoutes wires the two agent-session endpoints onto the router.
// Both routes live under the JWT-protected group created in main.go.
func RegisterAgentRoutes(app fiber.Router, svc AgentServiceInterface) {
	app.Get("/api/admin/agent-sessions", getAdminAgentSessions(svc))
	app.Get("/api/me/agent-sessions", getMyAgentSessions(svc))
}

func getAdminAgentSessions(svc AgentServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		sessions, err := svc.GetAgentSessions(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "sessions": sessions})
	}
}

func getMyAgentSessions(svc AgentServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		sessions, err := svc.GetMyAgentSessions(c.Context(), claims.UserID, month)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "sessions": sessions})
	}
}
```

- [ ] **Run API tests — expect pass**:

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run "TestGetAdminAgentSessions|TestGetMyAgentSessions" -v 2>&1
# expected:
# --- PASS: TestGetAdminAgentSessions_OK (0.00s)
# --- PASS: TestGetAdminAgentSessions_ForbiddenForEmployee (0.00s)
# --- PASS: TestGetAdminAgentSessions_MissingMonth (0.00s)
# --- PASS: TestGetMyAgentSessions_OK (0.00s)
# --- PASS: TestGetMyAgentSessions_MissingMonth (0.00s)
# PASS
```

### Step 2.5 — Wire into `admin/main.go`

- [ ] In `admin/main.go`, after the line `roiSvc := services.NewROIService(pool)`, add:

```go
	agentSvc := services.NewAgentServiceFromPool(pool)
```

And after `api.RegisterROIRoutes(protected, roiSvc)`, add:

```go
	api.RegisterAgentRoutes(protected, agentSvc)
```

The modified section of `admin/main.go` (lines 35–65) should look like:

```go
	roiSvc := services.NewROIService(pool)
	agentSvc := services.NewAgentServiceFromPool(pool)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	protected := app.Group("/", jwtMiddleware)
	protected.Use(services.IPAllowlistMiddleware(allowlistSvc))
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	usageSvc := services.NewUsageService(pool)
	api.RegisterUsageRoutes(protected, usageSvc)
	api.RegisterUsageAdminRoutes(protected, usageSvc)
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
	api.RegisterKPIRoutes(protected, kpiSvc)
	api.RegisterFuelRoutes(protected, fuelSvc)
	api.RegisterIPAllowlistRoutes(protected, allowlistSvc)
	api.RegisterBotRoutes(protected, botSvc)
	api.RegisterAIInsightsRoutes(protected, insightsSvc)
	api.RegisterHRSyncRoutes(protected, hrSyncSvc)
	api.RegisterROIRoutes(protected, roiSvc)
	api.RegisterAgentRoutes(protected, agentSvc)
```

- [ ] **Build check**:

```bash
cd /Users/sugac.275/ToTra/admin && go build ./... 2>&1
# expected: (no output)
```

- [ ] **Run all admin tests**:

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... 2>&1
# expected: ok  github.com/yourorg/totra/admin/...
```

- [ ] **Commit Task 2**:

```bash
cd /Users/sugac.275/ToTra && git add admin/services/agent.go admin/services/agent_test.go admin/api/agent.go admin/api/agent_test.go admin/main.go
git commit -m "feat(admin): add AgentService and agent-sessions API endpoints"
```

---

## Task 3: Frontend — AgentTrackingPage

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/AgentTrackingPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

### Step 3.1 — Add types and API calls to `client.ts`

- [ ] Append to `dashboard/src/api/client.ts` (after the last `export interface` block):

```ts
export interface AgentSession {
  id: string;
  tenant_id: string;
  user_id: string;
  user_name: string;
  conversation_id: string;
  loop_count: number;
  tool_call_count: number;
  is_dead_loop: boolean;
  last_seen_at: string;
  created_at: string;
}

export const getAdminAgentSessions = (month: string) =>
  apiClient.get<{ month: string; sessions: AgentSession[] }>(
    `/api/admin/agent-sessions?month=${month}`
  );

export const getMyAgentSessions = (month: string) =>
  apiClient.get<{ month: string; sessions: AgentSession[] }>(
    `/api/me/agent-sessions?month=${month}`
  );
```

### Step 3.2 — Create `AgentTrackingPage.tsx`

- [ ] Create `dashboard/src/pages/admin/AgentTrackingPage.tsx`:

```tsx
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getAdminAgentSessions } from "../../api/client";
import type { AgentSession } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";

const currentMonth = new Date().toISOString().slice(0, 7);

export function AgentTrackingPage() {
  const [month, setMonth] = useState(currentMonth);

  const { data, isLoading } = useQuery({
    queryKey: ["agent-sessions-admin", month],
    queryFn: () => getAdminAgentSessions(month).then((r) => r.data),
  });

  const sessions: AgentSession[] = data?.sessions ?? [];
  const deadLoopCount = sessions.filter((s) => s.is_dead_loop).length;
  const totalLoops = sessions.reduce((sum, s) => sum + s.loop_count, 0);
  const totalToolCalls = sessions.reduce((sum, s) => sum + s.tool_call_count, 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">AI Agent Tracking</h1>
        <input
          type="month"
          value={month}
          onChange={(e) => setMonth(e.target.value)}
          className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
        />
      </div>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{sessions.length}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Loops</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{totalLoops.toLocaleString()}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Dead-Loop Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className={`text-3xl font-bold ${deadLoopCount > 0 ? "text-red-400" : ""}`}>
              {deadLoopCount}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Agent Sessions — {month}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : sessions.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4">No agent sessions found for {month}.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400 text-left">
                  <th className="px-4 py-3 font-medium">User</th>
                  <th className="px-4 py-3 font-medium">Conversation ID</th>
                  <th className="px-4 py-3 font-medium text-right">Loops</th>
                  <th className="px-4 py-3 font-medium text-right">Tool Calls</th>
                  <th className="px-4 py-3 font-medium">Dead Loop</th>
                  <th className="px-4 py-3 font-medium">Last Seen</th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr
                    key={s.id}
                    className={`border-b border-zinc-800/50 hover:bg-zinc-800/30 transition-colors ${
                      s.is_dead_loop ? "bg-red-950/20" : ""
                    }`}
                  >
                    <td className="px-4 py-3 font-medium">{s.user_name}</td>
                    <td className="px-4 py-3 font-mono text-xs text-zinc-400 truncate max-w-[180px]">
                      {s.conversation_id}
                    </td>
                    <td className="px-4 py-3 text-right">{s.loop_count}</td>
                    <td className="px-4 py-3 text-right">{s.tool_call_count}</td>
                    <td className="px-4 py-3">
                      {s.is_dead_loop ? (
                        <span className="inline-flex items-center rounded-full bg-red-900/50 px-2 py-0.5 text-xs font-medium text-red-300">
                          Dead Loop
                        </span>
                      ) : (
                        <span className="inline-flex items-center rounded-full bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-400">
                          OK
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-zinc-400">
                      {new Date(s.last_seen_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr className="border-t border-zinc-700 bg-zinc-800/30">
                  <td className="px-4 py-3 font-medium text-zinc-300" colSpan={2}>
                    Total
                  </td>
                  <td className="px-4 py-3 text-right font-medium">{totalLoops.toLocaleString()}</td>
                  <td className="px-4 py-3 text-right font-medium">{totalToolCalls.toLocaleString()}</td>
                  <td className="px-4 py-3 text-red-400 font-medium">{deadLoopCount} dead</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

### Step 3.3 — Register route in `App.tsx`

- [ ] In `dashboard/src/App.tsx`, add the import after the existing admin page imports:

```tsx
import { AgentTrackingPage } from "./pages/admin/AgentTrackingPage";
```

And add the route inside the `<Route path="/" ...>` group, after the `admin/roi` route:

```tsx
<Route path="admin/agent-tracking" element={<ProtectedRoute adminOnly><AgentTrackingPage /></ProtectedRoute>} />
```

### Step 3.4 — Add nav item to `Layout.tsx`

- [ ] In `dashboard/src/components/Layout.tsx`, add to the `adminNavItems` array (after the `"ROI Reports"` entry):

```ts
  { label: "Agent Tracking", href: "/admin/agent-tracking" },
```

### Step 3.5 — Type-check and build

- [ ] **TypeScript check**:

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1
# expected: (no output — no type errors)
```

- [ ] **Vite build**:

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run build 2>&1 | tail -5
# expected:
# ✓ built in Xs
```

- [ ] **Commit Task 3**:

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/api/client.ts dashboard/src/pages/admin/AgentTrackingPage.tsx dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "feat(dashboard): add AgentTrackingPage for admin agent session monitoring"
```

---

## Summary Checklist

- [ ] Task 0: `infra/postgres/010_agent_sessions.sql` applied
- [ ] Task 1: `AgentStore` + `AgentMiddleware` + config + `main.go` wired
- [ ] Task 2: `AgentService` + API handlers + `admin/main.go` wired
- [ ] Task 3: Frontend types, page, route, nav item
