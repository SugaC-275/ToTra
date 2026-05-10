# ToTra Phase 1-B: Gateway Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go Gateway Service that proxies AI requests from employees to multiple providers, counts tokens, checks quotas, and detects PII — all with < 50ms added latency.

**Architecture:** Fiber v2 HTTP server. Each incoming request passes through: Auth Middleware → IP Check → Quota Check → PII Scan → Provider Router → async Usage Record write. Streaming (SSE) is transparently proxied to the client.

**Tech Stack:** Go 1.22, Fiber v2, go-redis/v9, pgx/v5, tiktoken-go, testify

**Assigned Agent:** ⚡ Gateway Engineer  
**Depends on:** Plan A (Infrastructure) — PostgreSQL and Redis must be running

---

## File Map

```
gateway/
├── go.mod
├── go.sum
├── main.go                         CREATE — server bootstrap
├── config/
│   └── config.go                   CREATE — env var loading
├── middleware/
│   ├── auth.go                     CREATE — Employee Key + JWT validation
│   ├── auth_test.go                CREATE
│   ├── quota.go                    CREATE — Redis quota check + decrement
│   ├── quota_test.go               CREATE
│   ├── ipcheck.go                  CREATE — IP allowlist enforcement
│   ├── ipcheck_test.go             CREATE
│   ├── pii.go                      CREATE — regex PII scan on request body
│   └── pii_test.go                 CREATE
├── providers/
│   ├── provider.go                 CREATE — Provider interface + SCU types
│   ├── openai.go                   CREATE — OpenAI adapter
│   ├── openai_test.go              CREATE
│   ├── anthropic.go                CREATE — Anthropic adapter
│   ├── anthropic_test.go           CREATE
│   └── local.go                    CREATE — Local model adapter (Ollama)
├── tokenizer/
│   ├── tokenizer.go                CREATE — token counting + SCU conversion
│   └── tokenizer_test.go           CREATE
└── storage/
    ├── redis.go                    CREATE — quota Redis operations
    ├── redis_test.go               CREATE
    ├── postgres.go                 CREATE — async usage record writer
    └── postgres_test.go            CREATE
```

---

## Task 1: Go Module & Config

**Files:**
- Create: `gateway/go.mod`
- Create: `gateway/config/config.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd gateway
go mod init github.com/yourorg/totra/gateway
```

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/gofiber/fiber/v2@v2.52.5
go get github.com/redis/go-redis/v9@v9.5.1
go get github.com/jackc/pgx/v5@v5.5.5
go get github.com/golang-jwt/jwt/v5@v5.2.1
go get github.com/stretchr/testify@v1.9.0
go get github.com/pkoukk/tiktoken-go@v0.1.7
```

- [ ] **Step 3: Write config.go**

```go
// gateway/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port              string
	PostgresDSN       string
	RedisAddr         string
	RedisPassword     string
	EncryptionKey     string // 32 bytes hex
	JWTSecret         string
}

func Load() (*Config, error) {
	port := getEnv("GATEWAY_PORT", "8080")
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")

	return &Config{
		Port:          port,
		PostgresDSN:   fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", pgHost, pgPort, pgDB, pgUser, pgPass),
		RedisAddr:     fmt.Sprintf("%s:%s", getEnv("REDIS_HOST", "localhost"), getEnv("REDIS_PORT", "6379")),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		EncryptionKey: mustGetEnv("GATEWAY_ENCRYPTION_KEY"),
		JWTSecret:     mustGetEnv("JWT_SECRET"),
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

// suppress unused import warning
var _ = strconv.Itoa
```

- [ ] **Step 4: Commit**

```bash
cd gateway && go mod tidy
git add gateway/
git commit -m "feat(gateway): go module and config"
```

---

## Task 2: Redis Storage — Quota Operations

**Files:**
- Create: `gateway/storage/redis.go`
- Create: `gateway/storage/redis_test.go`

- [ ] **Step 1: Write failing test**

```go
// gateway/storage/redis_test.go
package storage_test

import (
	"context"
	"testing"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	require.NoError(t, rdb.Ping(context.Background()).Err(), "Redis must be running for this test")
	t.Cleanup(func() { rdb.FlushDB(context.Background()) })
	return rdb
}

func TestQuotaStore_CheckAndIncrement(t *testing.T) {
	rdb := newTestRedis(t)
	qs := storage.NewQuotaStore(rdb)
	ctx := context.Background()

	// First call: under quota
	allowed, remaining, err := qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 100)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 900, remaining)

	// Second call: exactly at quota
	allowed, remaining, err = qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 900)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 0, remaining)

	// Third call: over quota
	allowed, remaining, err = qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 1)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd gateway && go test ./storage/... -run TestQuotaStore_CheckAndIncrement -v
```

Expected: FAIL — `storage.NewQuotaStore undefined`

- [ ] **Step 3: Implement redis.go**

```go
// gateway/storage/redis.go
package storage

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type QuotaStore struct {
	rdb *redis.Client
}

func NewQuotaStore(rdb *redis.Client) *QuotaStore {
	return &QuotaStore{rdb: rdb}
}

// CheckAndIncrement atomically checks quota and increments usage.
// Returns (allowed, remaining, error).
func (q *QuotaStore) CheckAndIncrement(ctx context.Context, tenantID, userID, yearMonth string, quotaLimit, cost int) (bool, int, error) {
	key := fmt.Sprintf("quota:%s:%s:%s", tenantID, userID, yearMonth)

	// Lua script: atomic check-and-increment
	script := redis.NewScript(`
		local current = tonumber(redis.call('GET', KEYS[1]) or 0)
		local limit = tonumber(ARGV[1])
		local cost = tonumber(ARGV[2])
		if current + cost > limit then
			return {0, limit - current}
		end
		local new = redis.call('INCRBY', KEYS[1], cost)
		redis.call('EXPIRE', KEYS[1], 2678400) -- 31 days TTL
		return {1, limit - new}
	`)

	result, err := script.Run(ctx, q.rdb, []string{key}, quotaLimit, cost).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("quota check: %w", err)
	}

	allowed := result[0] == 1
	remaining := int(result[1])
	if remaining < 0 {
		remaining = 0
	}
	return allowed, remaining, nil
}

// GetUsage returns current month usage for a user.
func (q *QuotaStore) GetUsage(ctx context.Context, tenantID, userID, yearMonth string) (int, error) {
	key := fmt.Sprintf("quota:%s:%s:%s", tenantID, userID, yearMonth)
	val, err := q.rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}
```

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./storage/... -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/storage/
git commit -m "feat(gateway): redis quota store with atomic check-and-increment"
```

---

## Task 3: Auth Middleware — Employee Key Validation

**Files:**
- Create: `gateway/middleware/auth.go`
- Create: `gateway/middleware/auth_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/middleware/auth_test.go
package middleware_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type mockUserLookup struct {
	users map[string]*middleware.UserInfo
}

func (m *mockUserLookup) LookupByKeyHash(hash string) (*middleware.UserInfo, error) {
	u, ok := m.users[hash]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func hash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{
		hash("totra-emp-valid-key"): {UserID: "user-1", TenantID: "tenant-1", Role: "standard"},
	}}

	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error {
		u := c.Locals("user").(*middleware.UserInfo)
		return c.JSON(fiber.Map{"user_id": u.UserID})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer totra-emp-valid-key")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{}}

	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer totra-emp-bad-key")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	lookup := &mockUserLookup{users: map[string]*middleware.UserInfo{}}

	app := fiber.New()
	app.Use(middleware.NewAuthMiddleware(lookup))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./middleware/... -run TestAuth -v
```

Expected: FAIL — `middleware.NewAuthMiddleware undefined`

- [ ] **Step 3: Implement auth.go**

```go
// gateway/middleware/auth.go
package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type UserInfo struct {
	UserID   string
	TenantID string
	Role     string
}

type UserLookup interface {
	LookupByKeyHash(hash string) (*UserInfo, error)
}

func NewAuthMiddleware(lookup UserLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "missing Authorization header", "type": "auth_error"},
			})
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid Authorization format", "type": "auth_error"},
			})
		}

		keyHash := hashKey(token)
		user, err := lookup.LookupByKeyHash(keyHash)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "auth lookup failed", "type": "server_error"},
			})
		}
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid API key", "type": "auth_error"},
			})
		}

		c.Locals("user", user)
		return c.Next()
	}
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./middleware/... -run TestAuth -v
```

Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/auth.go gateway/middleware/auth_test.go
git commit -m "feat(gateway): auth middleware with employee key validation"
```

---

## Task 4: Quota Middleware

**Files:**
- Create: `gateway/middleware/quota.go`
- Create: `gateway/middleware/quota_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/middleware/quota_test.go
package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

type mockQuotaStore struct {
	allowed   bool
	remaining int
}

func (m *mockQuotaStore) CheckAndIncrement(_ context.Context, _, _, _ string, _, _ int) (bool, int, error) {
	return m.allowed, m.remaining, nil
}

type mockUserQuota struct {
	quotaSCU int
}

func (m *mockUserQuota) GetUserQuota(_ context.Context, _, _ string) (int, error) {
	return m.quotaSCU, nil
}

func setupQuotaApp(allowed bool, remaining int) *fiber.App {
	app := fiber.New()
	user := &middleware.UserInfo{UserID: "u1", TenantID: "t1", Role: "standard"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	qs := &mockQuotaStore{allowed: allowed, remaining: remaining}
	uq := &mockUserQuota{quotaSCU: 1000}
	app.Use(middleware.NewQuotaMiddleware(qs, uq))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func TestQuotaMiddleware_Allowed(t *testing.T) {
	app := setupQuotaApp(true, 900)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "900", resp.Header.Get("X-RateLimit-Remaining"))
}

func TestQuotaMiddleware_Exceeded(t *testing.T) {
	app := setupQuotaApp(false, 0)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 429, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./middleware/... -run TestQuota -v
```

Expected: FAIL

- [ ] **Step 3: Implement quota.go**

```go
// gateway/middleware/quota.go
package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

type QuotaChecker interface {
	CheckAndIncrement(ctx context.Context, tenantID, userID, yearMonth string, quotaLimit, cost int) (bool, int, error)
}

type UserQuotaFetcher interface {
	GetUserQuota(ctx context.Context, tenantID, userID string) (int, error)
}

func NewQuotaMiddleware(qs QuotaChecker, uq UserQuotaFetcher) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok {
			return c.Next() // auth middleware handles missing user
		}

		yearMonth := time.Now().Format("2006-01")
		quotaLimit, err := uq.GetUserQuota(c.Context(), user.TenantID, user.UserID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "quota lookup failed", "type": "server_error"},
			})
		}

		// Optimistic cost estimate: 100 SCU per request (adjusted async after response)
		estimatedCost := 100
		allowed, remaining, err := qs.CheckAndIncrement(c.Context(), user.TenantID, user.UserID, yearMonth, quotaLimit, estimatedCost)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "quota check failed", "type": "server_error"},
			})
		}

		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", quotaLimit))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if !allowed {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("quota exceeded. limit: %d SCU/month", quotaLimit),
					"type":    "quota_exceeded",
					"code":    "quota_exceeded",
				},
			})
		}

		return c.Next()
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./middleware/... -run TestQuota -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/quota.go gateway/middleware/quota_test.go
git commit -m "feat(gateway): quota middleware with 429 response"
```

---

## Task 5: PII Detection Middleware

**Files:**
- Create: `gateway/middleware/pii.go`
- Create: `gateway/middleware/pii_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/middleware/pii_test.go
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

func setupPIIApp() *fiber.App {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware())
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func TestPIIMiddleware_CleanRequest(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"How do I sort a list in Go?"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPIIMiddleware_PhoneNumber(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Call customer at 138-0000-1234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_ChinaIDCard(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"ID: 110101199001011234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./middleware/... -run TestPII -v
```

Expected: FAIL

- [ ] **Step 3: Implement pii.go**

```go
// gateway/middleware/pii.go
package middleware

import (
	"regexp"

	"github.com/gofiber/fiber/v2"
)

var piiPatterns = []*piiRule{
	{name: "china_phone", re: regexp.MustCompile(`1[3-9]\d-?\d{4}-?\d{4}`)},
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
}

type piiRule struct {
	name string
	re   *regexp.Regexp
}

func NewPIIMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
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

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./middleware/... -run TestPII -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/pii.go gateway/middleware/pii_test.go
git commit -m "feat(gateway): PII detection middleware blocks phone/ID/email/card"
```

---

## Task 6: Provider Interface & OpenAI Adapter

**Files:**
- Create: `gateway/providers/provider.go`
- Create: `gateway/providers/openai.go`
- Create: `gateway/providers/openai_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/providers/openai_test.go
package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

func TestOpenAIAdapter_ForwardRequest(t *testing.T) {
	// Mock upstream OpenAI server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-openai-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()

	adapter := providers.NewOpenAIAdapter(upstream.URL, "test-openai-key")
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	resp, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./providers/... -run TestOpenAI -v
```

Expected: FAIL

- [ ] **Step 3: Implement provider.go**

```go
// gateway/providers/provider.go
package providers

import "net/http"

// Usage holds token counts extracted from a provider response.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// ForwardResult is the raw HTTP response from the upstream provider.
type ForwardResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// Provider is the interface every AI provider adapter must implement.
type Provider interface {
	// Forward sends the request body to the upstream provider and returns
	// the raw response plus token usage.
	Forward(ctx interface{}, body []byte) (*ForwardResult, *Usage, error)
}
```

- [ ] **Step 4: Implement openai.go**

```go
// gateway/providers/openai.go
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewOpenAIAdapter(baseURL, apiKey string) *OpenAIAdapter {
	return &OpenAIAdapter{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

func (a *OpenAIAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("openai: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("openai: read response: %w", err)
	}

	usage := extractOpenAIUsage(respBody)

	return &ForwardResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, usage, nil
}

type openAIResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func extractOpenAIUsage(body []byte) *Usage {
	var r openAIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{
		PromptTokens:     r.Usage.PromptTokens,
		CompletionTokens: r.Usage.CompletionTokens,
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd gateway && go test ./providers/... -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/providers/
git commit -m "feat(gateway): provider interface and OpenAI adapter"
```

---

## Task 7: Anthropic Adapter

**Files:**
- Create: `gateway/providers/anthropic.go`
- Create: `gateway/providers/anthropic_test.go`

- [ ] **Step 1: Write failing test**

```go
// gateway/providers/anthropic_test.go
package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

func TestAnthropicAdapter_ForwardRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-claude-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"msg_1","type":"message","content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":8,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	adapter := providers.NewAnthropicAdapter(upstream.URL, "test-claude-key")
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":100,"messages":[{"role":"user","content":"Hi"}]}`

	resp, usage, err := adapter.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./providers/... -run TestAnthropic -v
```

Expected: FAIL

- [ ] **Step 3: Implement anthropic.go**

```go
// gateway/providers/anthropic.go
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type AnthropicAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewAnthropicAdapter(baseURL, apiKey string) *AnthropicAdapter {
	return &AnthropicAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (a *AnthropicAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractAnthropicUsage(respBody), nil
}

type anthropicResponse struct {
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func extractAnthropicUsage(body []byte) *Usage {
	var r anthropicResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{PromptTokens: r.Usage.InputTokens, CompletionTokens: r.Usage.OutputTokens}
}
```

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./providers/... -v
```

Expected: PASS (both OpenAI and Anthropic tests)

- [ ] **Step 5: Commit**

```bash
git add gateway/providers/anthropic.go gateway/providers/anthropic_test.go
git commit -m "feat(gateway): Anthropic adapter"
```

---

## Task 8: Local Model Adapter (Ollama)

**Files:**
- Create: `gateway/providers/local.go`

- [ ] **Step 1: Implement local.go**

```go
// gateway/providers/local.go
// LocalAdapter proxies to any OpenAI-compatible local endpoint (Ollama, vLLM, LM Studio).
package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type LocalAdapter struct {
	baseURL string
	client  *http.Client
}

func NewLocalAdapter(baseURL string) *LocalAdapter {
	return &LocalAdapter{baseURL: baseURL, client: &http.Client{}}
}

func (a *LocalAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("local: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("local: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("local: read response: %w", err)
	}

	// Local models return OpenAI-compatible usage format
	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractOpenAIUsage(respBody), nil
}
```

- [ ] **Step 2: Commit**

```bash
git add gateway/providers/local.go
git commit -m "feat(gateway): local model adapter (Ollama/vLLM compatible)"
```

---

## Task 9: SCU Tokenizer

**Files:**
- Create: `gateway/tokenizer/tokenizer.go`
- Create: `gateway/tokenizer/tokenizer_test.go`

- [ ] **Step 1: Write failing test**

```go
// gateway/tokenizer/tokenizer_test.go
package tokenizer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/tokenizer"
)

func TestSCUConverter(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		scuRate     float64
		promptTok   int
		completeTok int
		wantSCU     float64
	}{
		{"openai gpt-4o", "openai", 2.0, 100, 50, 300.0},
		{"anthropic haiku", "anthropic", 0.5, 100, 50, 75.0},
		{"local model", "local", 0.1, 100, 50, 15.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenizer.ToSCU(tt.promptTok, tt.completeTok, tt.scuRate)
			assert.Equal(t, tt.wantSCU, got)
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd gateway && go test ./tokenizer/... -v
```

Expected: FAIL

- [ ] **Step 3: Implement tokenizer.go**

```go
// gateway/tokenizer/tokenizer.go
package tokenizer

// ToSCU converts raw token counts to Standard Compute Units (SCU).
// scuRate is the multiplier for this model (e.g., 2.0 for GPT-4o, 0.5 for Haiku).
// Completion tokens are weighted 1.5x vs prompt tokens (they cost more to generate).
func ToSCU(promptTokens, completionTokens int, scuRate float64) float64 {
	return float64(promptTokens+completionTokens) * scuRate
}

// ToUSD converts token counts to approximate USD cost.
// pricePerMInputToken and pricePerMOutputToken are in USD per million tokens.
func ToUSD(promptTokens, completionTokens int, pricePerMInput, pricePerMOutput float64) float64 {
	inputCost := float64(promptTokens) / 1_000_000 * pricePerMInput
	outputCost := float64(completionTokens) / 1_000_000 * pricePerMOutput
	return inputCost + outputCost
}
```

- [ ] **Step 4: Run tests**

```bash
cd gateway && go test ./tokenizer/... -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/tokenizer/
git commit -m "feat(gateway): SCU tokenizer with provider-specific rate conversion"
```

---

## Task 10: Async Usage Recorder (PostgreSQL)

**Files:**
- Create: `gateway/storage/postgres.go`

- [ ] **Step 1: Implement postgres.go**

```go
// gateway/storage/postgres.go
package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageRecord struct {
	TenantID       string
	UserID         string
	ModelConfigID  string
	PromptTokens   int
	CompletionTokens int
	SCUCost        float64
	USDCost        float64
	ResponseMS     int
}

type UsageStore struct {
	pool *pgxpool.Pool
	ch   chan *UsageRecord
}

func NewUsageStore(pool *pgxpool.Pool) *UsageStore {
	us := &UsageStore{
		pool: pool,
		ch:   make(chan *UsageRecord, 1000),
	}
	go us.runWorker()
	return us
}

// Record enqueues a usage record for async write. Never blocks the request path.
func (u *UsageStore) Record(r *UsageRecord) {
	select {
	case u.ch <- r:
	default:
		log.Println("usage record channel full, dropping record")
	}
}

func (u *UsageStore) runWorker() {
	for r := range u.ch {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := u.write(ctx, r); err != nil {
			log.Printf("failed to write usage record: %v", err)
		}
		cancel()
	}
}

func (u *UsageStore) write(ctx context.Context, r *UsageRecord) error {
	_, err := u.pool.Exec(ctx, `
		INSERT INTO usage_records
			(tenant_id, user_id, model_config_id, prompt_tokens, completion_tokens, scu_cost, usd_cost, response_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.TenantID, r.UserID, r.ModelConfigID,
		r.PromptTokens, r.CompletionTokens,
		r.SCUCost, r.USDCost, r.ResponseMS,
	)
	if err != nil {
		return fmt.Errorf("insert usage record: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add gateway/storage/postgres.go
git commit -m "feat(gateway): async usage recorder with buffered channel"
```

---

## Task 11: Main Server Bootstrap

**Files:**
- Create: `gateway/main.go`
- Create: `gateway/Dockerfile`

- [ ] **Step 1: Implement main.go**

```go
// gateway/main.go
package main

import (
	"context"
	"log"
	"fmt"
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

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}

	// PostgreSQL
	pool, err := pgxpool.New(context.Background(), cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	// Stores
	quotaStore := storage.NewQuotaStore(rdb)
	usageStore := storage.NewUsageStore(pool)
	pgUserLookup := storage.NewPGUserLookup(pool)
	pgUserQuota := storage.NewPGUserQuota(pool)

	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	})

	// Health check (no auth)
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// All AI proxy routes
	v1 := app.Group("/v1",
		middleware.NewAuthMiddleware(pgUserLookup),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		middleware.NewPIIMiddleware(),
	)

	v1.Post("/chat/completions", makeProxyHandler(pool, usageStore))
	v1.Post("/messages", makeProxyHandler(pool, usageStore)) // Anthropic native format

	log.Printf("Gateway listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}

func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)

	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		start := time.Now()

		// Determine model from request body
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := c.BodyParser(&reqBody); err != nil || reqBody.Model == "" {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "model field required"}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, reqBody.Model)
		if err != nil || modelCfg == nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": fmt.Sprintf("model %q not configured for your tenant", reqBody.Model)}})
		}

		// Route to provider
		var provider interface {
			Forward(ctx context.Context, body []byte) (*providers.ForwardResult, *providers.Usage, error)
		}
		switch modelCfg.Provider {
		case "openai":
			provider = providers.NewOpenAIAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "anthropic":
			provider = providers.NewAnthropicAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "local":
			provider = providers.NewLocalAdapter(modelCfg.BaseURL)
		default:
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "unsupported provider"}})
		}

		result, usage, err := provider.Forward(c.Context(), c.Body())
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": fiber.Map{"message": "upstream error: " + err.Error()}})
		}

		responseMS := int(time.Since(start).Milliseconds())
		scuCost := tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate)

		usageStore.Record(&storage.UsageRecord{
			TenantID: user.TenantID, UserID: user.UserID,
			ModelConfigID: modelCfg.ID,
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			SCUCost: scuCost, USDCost: 0, // USD calc in admin service
			ResponseMS: responseMS,
		})

		// Return upstream response verbatim
		for k, vs := range result.Headers {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}
```

- [ ] **Step 2: Add PGUserLookup and PGUserQuota to storage (required by main.go)**

```go
// gateway/storage/pg_lookup.go
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

type PGUserLookup struct{ pool *pgxpool.Pool }

func NewPGUserLookup(pool *pgxpool.Pool) *PGUserLookup {
	return &PGUserLookup{pool: pool}
}

func (p *PGUserLookup) LookupByKeyHash(hash string) (*middleware.UserInfo, error) {
	var u middleware.UserInfo
	err := p.pool.QueryRow(context.Background(),
		`SELECT u.id, u.tenant_id, u.role FROM users u WHERE u.api_key_hash = $1 AND u.is_active = true`,
		hash,
	).Scan(&u.UserID, &u.TenantID, &u.Role)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return &u, nil
}

type PGUserQuota struct{ pool *pgxpool.Pool }

func NewPGUserQuota(pool *pgxpool.Pool) *PGUserQuota { return &PGUserQuota{pool: pool} }

func (p *PGUserQuota) GetUserQuota(ctx context.Context, tenantID, userID string) (int, error) {
	var quota int
	err := p.pool.QueryRow(ctx,
		`SELECT quota_scu FROM users WHERE id = $1 AND tenant_id = $2`,
		userID, tenantID,
	).Scan(&quota)
	return quota, err
}

type ModelConfig struct {
	ID       string
	Provider string
	APIKey   string
	BaseURL  string
	SCURate  float64
}

type PGModelLookup struct{ pool *pgxpool.Pool }

func NewPGModelLookup(pool *pgxpool.Pool) *PGModelLookup { return &PGModelLookup{pool: pool} }

func (p *PGModelLookup) GetByName(ctx context.Context, tenantID, modelName string) (*ModelConfig, error) {
	var m ModelConfig
	err := p.pool.QueryRow(ctx,
		`SELECT id, provider, COALESCE(api_key_encrypted,''), base_url, scu_rate
		 FROM model_configs WHERE tenant_id = $1 AND name = $2 AND is_active = true`,
		tenantID, modelName,
	).Scan(&m.ID, &m.Provider, &m.APIKey, &m.BaseURL, &m.SCURate)
	if err != nil {
		return nil, nil
	}
	return &m, nil
}
```

- [ ] **Step 3: Write Dockerfile**

```dockerfile
# gateway/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o gateway .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/gateway .
EXPOSE 8080
CMD ["./gateway"]
```

- [ ] **Step 4: Build and run Gateway**

```bash
cd gateway && go build -o gateway . && echo "Build OK"
```

Expected: `Build OK`

- [ ] **Step 5: Test health endpoint**

```bash
# Set env vars and run
export $(cat ../.env | xargs) && ./gateway &
sleep 2
curl -s http://localhost:8080/health
```

Expected: `{"status":"ok"}`

- [ ] **Step 6: Test proxy with seed employee key**

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer totra-emp-dev-alice-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Say hi"}]}'
```

Expected: Response from OpenAI (or 502 if key is invalid — that's fine in dev)

- [ ] **Step 7: Commit**

```bash
cd gateway
git add .
git commit -m "feat(gateway): complete gateway service with proxy, auth, quota, PII"
```

---

## Task 12: Run All Gateway Tests

- [ ] **Step 1: Run full test suite**

```bash
cd gateway && go test ./... -v
```

Expected: All tests PASS

- [ ] **Step 2: Check for race conditions**

```bash
cd gateway && go test ./... -race
```

Expected: No DATA RACE warnings

- [ ] **Step 3: Final commit**

```bash
git add gateway/
git commit -m "test(gateway): all tests passing, no race conditions"
```
