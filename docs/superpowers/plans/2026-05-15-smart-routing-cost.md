# Smart Routing + Cost Precision Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the gateway's model router from a single body-length heuristic to a five-signal complexity score (0–100), and back-fill every routing event with token counts and USD savings calculated from per-model pricing stored in `model_configs`.

**Architecture:** Gateway `router.go` computes a `complexityScore` from body size, message count, system-prompt presence, tool-calls, and complex keywords; if score < `ROUTER_COMPLEXITY_THRESHOLD` (default 50), the request is downgraded and the routing event ID is stored in Fiber's `c.Locals`. After `Forward()` returns, `makeProxyHandler` fires an async goroutine that calls `RoutingStore.UpdateTokensAndSavings` to write token counts and USD delta to the same row. Admins set per-model pricing via a new PUT endpoint; the cost/savings report surface the new fields.

**Tech Stack:** Go (Fiber, pgx/v5, os/strconv for env), TypeScript/React (Tanstack Query), PostgreSQL

---

## File Map

| Action | File |
|--------|------|
| Create | `infra/postgres/023_smart_routing_cost.sql` |
| Modify | `docker-compose.yml` |
| Modify | `gateway/middleware/router.go` |
| Modify | `gateway/middleware/router_test.go` |
| Modify | `gateway/storage/routing_store.go` |
| Modify | `gateway/storage/routing_store_test.go` |
| Modify | `gateway/storage/pg_lookup.go` |
| Modify | `gateway/main.go` |
| Modify | `admin/services/models.go` |
| Modify | `admin/services/cost_savings_report.go` |
| Modify | `admin/services/cost_savings_report_test.go` |
| Modify | `admin/api/models.go` |
| Modify | `admin/api/models_test.go` |
| Modify | `admin/api/cost_savings.go` |
| Modify | `admin/api/cost_savings_test.go` |
| Modify | `dashboard/src/api/client.ts` |
| Modify | `dashboard/src/pages/admin/CostCenterPage.tsx` |
| Modify | `dashboard/src/pages/admin/ModelsPage.tsx` |

---

### Task 1: DB Migration 023 — pricing columns + routing event fields

**Files:**
- Create: `infra/postgres/023_smart_routing_cost.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write the migration SQL**

Create `infra/postgres/023_smart_routing_cost.sql`:

```sql
-- Per-model pricing for USD savings calculation (NULL = no pricing configured)
ALTER TABLE model_configs
  ADD COLUMN IF NOT EXISTS price_per_m_input  NUMERIC(10,4),
  ADD COLUMN IF NOT EXISTS price_per_m_output NUMERIC(10,4);

-- Extended routing event fields backfilled after the upstream response
ALTER TABLE gateway_routing_events
  ADD COLUMN IF NOT EXISTS complexity_score   INT,
  ADD COLUMN IF NOT EXISTS prompt_tokens      INT,
  ADD COLUMN IF NOT EXISTS completion_tokens  INT,
  ADD COLUMN IF NOT EXISTS usd_saved          NUMERIC(12,6);
```

- [ ] **Step 2: Wire into docker-compose.yml**

In `docker-compose.yml`, inside the `postgres.volumes` list, add after the `022_siem.sql` line:

```yaml
      - ./infra/postgres/023_smart_routing_cost.sql:/docker-entrypoint-initdb.d/023_smart_routing_cost.sql:ro
```

- [ ] **Step 3: Verify**

```bash
cd /Users/sugac.275/ToTra && docker compose up postgres -d --wait && docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "\d model_configs" | grep price
```
Expected: two rows for `price_per_m_input` and `price_per_m_output`.

- [ ] **Step 4: Commit**

```bash
git add infra/postgres/023_smart_routing_cost.sql docker-compose.yml
git commit -m "feat(db): migration 023 — model pricing columns + routing event cost fields"
```

---

### Task 2: complexityScore function + router threshold logic

**Files:**
- Modify: `gateway/middleware/router.go`
- Modify: `gateway/middleware/router_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `gateway/middleware/router_test.go` (after existing tests):

```go
func TestComplexityScore_ShortCleanRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	score := middleware.ComplexityScore(body)
	assert.Less(t, score, 50, "short clean request should score below threshold")
}

func TestComplexityScore_LongBodyMaxesLengthSignal(t *testing.T) {
	longContent := strings.Repeat("x", 4000)
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"` + longContent + `"}]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 40, "body ≥ 4000 bytes should contribute 40 points")
}

func TestComplexityScore_SystemPromptAdds20(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"you are helpful"},{"role":"user","content":"hello"}]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 20, "system prompt should add 20 points")
}

func TestComplexityScore_ComplexKeywordAdds10(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"please analyze the situation"}]}`)
	scoreWith := middleware.ComplexityScore(body)
	bodyPlain := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"please describe the situation"}]}`)
	scorePlain := middleware.ComplexityScore(bodyPlain)
	assert.Equal(t, 10, scoreWith-scorePlain, "complex keyword should add exactly 10 points")
}

func TestComplexityScore_HighComplexityAboveThreshold(t *testing.T) {
	// system prompt (20) + 10 messages (20) + "analyze" keyword (10) + moderate length (~5) = 55+
	msgs := ""
	for i := 0; i < 10; i++ {
		msgs += `{"role":"user","content":"analyze step ` + strconv.Itoa(i) + `"},`
	}
	msgs += `{"role":"system","content":"you are a helpful assistant"}`
	body := []byte(`{"model":"claude-opus-4-7","messages":[` + msgs + `]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 50, "high-complexity request should score at or above default threshold")
}
```

Also update existing `captureRecorder` and `TestAutoRouter_PremiumModel_LongBody_NotDowngraded` in `router_test.go`:

```go
// Change captureRecorder.Record to match new signature:
type captureRecorder struct{ events []middleware.RoutingEvent }

func (r *captureRecorder) Record(_ context.Context, e middleware.RoutingEvent) (int64, error) {
	r.events = append(r.events, e)
	return 1, nil
}

// Replace TestAutoRouter_PremiumModel_LongBody_NotDowngraded with:
func TestAutoRouter_PremiumModel_HighComplexity_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	// system prompt (20) + 10 messages (20) + "analyze" (10) + ~400 bytes (4) = 54 → above threshold
	msgs := ""
	for i := 0; i < 10; i++ {
		msgs += `{"role":"user","content":"analyze step ` + strconv.Itoa(i) + `"},`
	}
	msgs += `{"role":"system","content":"system"}`
	body := `{"model":"claude-opus-4-7","messages":[` + msgs + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events), "high-complexity request must not be downgraded")
}
```

Also add `"strconv"` to the `router_test.go` import block.

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run "TestComplexityScore|TestAutoRouter" -v
```
Expected: FAIL — `middleware.ComplexityScore` undefined, `captureRecorder.Record` has wrong signature.

- [ ] **Step 3: Implement complexityScore + update RoutingRecorder + router logic**

Replace the contents of `gateway/middleware/router.go` with:

```go
package middleware

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type RoutingEvent struct {
	TenantID, UserID, OriginalModel, RoutedModel string
	BodyLen         int
	ComplexityScore int
}

type RoutingRecorder interface {
	Record(ctx context.Context, e RoutingEvent) (int64, error)
}

var premiumToStandard = map[string]string{
	"claude-opus-4-7": "claude-sonnet-4-6",
	"claude-opus-4-5": "claude-sonnet-4-6",
	"gpt-4-turbo":     "gpt-4o-mini",
	"o1":              "gpt-4o-mini",
	"o1-preview":      "gpt-4o-mini",
}

var complexKeywords = []string{
	"分析", "推理", "审查", "对比", "架构", "重构",
	"evaluate", "analyze", "compare", "refactor",
}

// ComplexityScore returns a 0–100 complexity estimate for the given request body.
// Exported so it can be tested independently of the middleware.
func ComplexityScore(body []byte) int {
	score := 0

	// Signal 1: body length (40 pts max)
	lenRatio := float64(len(body)) / 4000.0
	if lenRatio > 1.0 {
		lenRatio = 1.0
	}
	score += int(40 * lenRatio)

	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return score // length signal only on parse failure
	}

	// Signal 2: message count (20 pts max)
	msgRatio := float64(len(req.Messages)) / 10.0
	if msgRatio > 1.0 {
		msgRatio = 1.0
	}
	score += int(20 * msgRatio)

	// Signal 3: system prompt (20 pts)
	for _, m := range req.Messages {
		if m.Role == "system" {
			score += 20
			break
		}
	}

	// Signal 4: tool_calls field present (10 pts)
	bodyStr := string(body)
	if strings.Contains(bodyStr, `"tool_calls"`) || strings.Contains(bodyStr, `"tools"`) {
		score += 10
	}

	// Signal 5: complex keyword (10 pts)
	for _, kw := range complexKeywords {
		if strings.Contains(bodyStr, kw) {
			score += 10
			break
		}
	}

	return score
}

func routerThreshold() int {
	if v := os.Getenv("ROUTER_COMPLEXITY_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 50
}

func NewAutoRouterMiddleware(rec RoutingRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		score := ComplexityScore(body)
		if score >= routerThreshold() {
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
			id, err := rec.Record(c.Context(), RoutingEvent{
				TenantID: tid, UserID: uid,
				OriginalModel: reqBody.Model, RoutedModel: cheaper,
				BodyLen: len(body), ComplexityScore: score,
			})
			if err == nil && id > 0 {
				c.Locals("routing_event_id", id)
				c.Locals("original_model_name", reqBody.Model)
			}
		}
		return c.Next()
	}
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run "TestComplexityScore|TestAutoRouter" -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/router.go gateway/middleware/router_test.go
git commit -m "feat(gateway/router): complexityScore (5-signal 0-100) + ROUTER_COMPLEXITY_THRESHOLD env"
```

---

### Task 3: RoutingStore.Record returns ID + UpdateTokensAndSavings + ModelPrice

**Files:**
- Modify: `gateway/storage/routing_store.go`
- Modify: `gateway/storage/routing_store_test.go`
- Modify: `gateway/storage/pg_lookup.go`

- [ ] **Step 1: Write the failing tests**

Replace contents of `gateway/storage/routing_store_test.go`:

```go
package storage_test

import (
	"context"
	"testing"

	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// Interface compliance: RoutingStore must implement RoutingRecorder
// (which now returns (int64, error)).
func TestRoutingStore_ImplementsInterface(t *testing.T) {
	var _ middleware.RoutingRecorder = (*storage.RoutingStore)(nil)
}

func TestModelPrice_Fields(t *testing.T) {
	p := storage.ModelPrice{PricePerMInput: 5.0, PricePerMOutput: 15.0}
	if p.PricePerMInput != 5.0 || p.PricePerMOutput != 15.0 {
		t.Fatal("ModelPrice field assignment failed")
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run "TestRoutingStore|TestModelPrice" -v
```
Expected: FAIL — `RoutingStore.Record` has wrong return type, `ModelPrice` undefined.

- [ ] **Step 3: Update routing_store.go**

Replace the entire contents of `gateway/storage/routing_store.go`:

```go
package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// ModelPrice holds per-million-token pricing for a model.
type ModelPrice struct {
	PricePerMInput  float64
	PricePerMOutput float64
}

type RoutingStore struct{ pool *pgxpool.Pool }

func NewRoutingStore(pool *pgxpool.Pool) *RoutingStore { return &RoutingStore{pool: pool} }

// Record inserts a routing event synchronously and returns the new row ID.
// Synchronous insertion is required so the ID can be stored in c.Locals for
// later use by UpdateTokensAndSavings.
func (s *RoutingStore) Record(ctx context.Context, e middleware.RoutingEvent) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO gateway_routing_events
		 (tenant_id, user_id, original_model, routed_model, body_len_bytes, complexity_score)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		e.TenantID, e.UserID, e.OriginalModel, e.RoutedModel, e.BodyLen, e.ComplexityScore,
	).Scan(&id)
	if err != nil {
		log.Printf("routing_store: record: %v", err)
		return 0, err
	}
	return id, nil
}

// UpdateTokensAndSavings back-fills token counts and USD savings onto an existing
// routing event row after the upstream response has been received.
// Pass nil for orig or routed if either model has no pricing configured —
// usd_saved will be left NULL.
func (s *RoutingStore) UpdateTokensAndSavings(ctx context.Context, id int64, promptTokens, completionTokens int, orig, routed *ModelPrice) error {
	var usdSaved *float64
	if orig != nil && routed != nil {
		saved := float64(promptTokens)/1_000_000*(orig.PricePerMInput-routed.PricePerMInput) +
			float64(completionTokens)/1_000_000*(orig.PricePerMOutput-routed.PricePerMOutput)
		usdSaved = &saved
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE gateway_routing_events
		 SET prompt_tokens=$1, completion_tokens=$2, usd_saved=$3
		 WHERE id=$4`,
		promptTokens, completionTokens, usdSaved, id)
	if err != nil {
		log.Printf("routing_store: update tokens/savings id=%d: %v", id, err)
	}
	return err
}
```

- [ ] **Step 4: Add PricePerMInput/Output to gateway ModelConfig**

In `gateway/storage/pg_lookup.go`, update `ModelConfig` struct and `GetByName` query:

```go
type ModelConfig struct {
	ID              string
	Provider        string
	APIKey          string
	BaseURL         string
	SCURate         float64
	PricePerMInput  *float64 // nil when not configured
	PricePerMOutput *float64 // nil when not configured
}
```

And update `GetByName`:

```go
func (p *PGModelLookup) GetByName(ctx context.Context, tenantID, modelName string) (*ModelConfig, error) {
	var m ModelConfig
	err := p.pool.QueryRow(ctx,
		`SELECT id, provider, COALESCE(api_key_encrypted,''), base_url, scu_rate,
		        price_per_m_input, price_per_m_output
		 FROM model_configs
		 WHERE tenant_id = $1 AND name = $2 AND is_active = true`,
		tenantID, modelName,
	).Scan(&m.ID, &m.Provider, &m.APIKey, &m.BaseURL, &m.SCURate,
		&m.PricePerMInput, &m.PricePerMOutput)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get model config: %w", err)
	}
	return &m, nil
}
```

- [ ] **Step 5: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run "TestRoutingStore|TestModelPrice|TestSIEM" -v
```
Expected: all PASS.

- [ ] **Step 6: Verify gateway compiles**

```bash
cd /Users/sugac.275/ToTra/gateway && go build ./...
```
Expected: no errors (main.go will fail on `makeProxyHandler` — fix in Task 4).

- [ ] **Step 7: Commit**

```bash
git add gateway/storage/routing_store.go gateway/storage/routing_store_test.go gateway/storage/pg_lookup.go
git commit -m "feat(gateway/storage): RoutingStore.Record returns ID + UpdateTokensAndSavings + ModelPrice"
```

---

### Task 4: makeProxyHandler — wire routingStore + call UpdateTokensAndSavings

**Files:**
- Modify: `gateway/main.go`

- [ ] **Step 1: Update makeProxyHandler signature to accept routingStore**

In `gateway/main.go`, change the `proxyHandler` instantiation (around line 70):

```go
// before:
proxyHandler := makeProxyHandler(pool, usageStore, agentStore, requestCache)

// after:
proxyHandler := makeProxyHandler(pool, usageStore, agentStore, requestCache, routingStore)
```

- [ ] **Step 2: Update makeProxyHandler function signature**

Change the function signature at line ~84:

```go
func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore, cache *storage.RequestCache, routingStore *storage.RoutingStore) fiber.Handler {
```

- [ ] **Step 3: Add UpdateTokensAndSavings call inside makeProxyHandler**

After `result, usage, err := fwd.Forward(c.Context(), c.Body())` and before the early return on error, add the savings back-fill. Find the section right after `cache.Set(...)` and add:

```go
// Back-fill token counts and USD savings for routed requests.
if routingEventID, ok := c.Locals("routing_event_id").(int64); ok && routingEventID > 0 {
    originalModelName, _ := c.Locals("original_model_name").(string)
    if originalModelName != "" && usage != nil {
        origModelCfg, _ := modelLookup.GetByName(c.Context(), user.TenantID, originalModelName)
        var origPrice, routedPrice *storage.ModelPrice
        if origModelCfg != nil && origModelCfg.PricePerMInput != nil && origModelCfg.PricePerMOutput != nil {
            origPrice = &storage.ModelPrice{
                PricePerMInput:  *origModelCfg.PricePerMInput,
                PricePerMOutput: *origModelCfg.PricePerMOutput,
            }
        }
        if modelCfg.PricePerMInput != nil && modelCfg.PricePerMOutput != nil {
            routedPrice = &storage.ModelPrice{
                PricePerMInput:  *modelCfg.PricePerMInput,
                PricePerMOutput: *modelCfg.PricePerMOutput,
            }
        }
        go routingStore.UpdateTokensAndSavings(
            context.Background(), routingEventID,
            usage.PromptTokens, usage.CompletionTokens,
            origPrice, routedPrice,
        )
    }
}
```

This block should be placed immediately after `if result.StatusCode == 200 { cache.Set(...) }`.

- [ ] **Step 4: Verify gateway compiles and all tests pass**

```bash
cd /Users/sugac.275/ToTra/gateway && go build ./... && go test ./...
```
Expected: compiles cleanly, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/main.go
git commit -m "feat(gateway): wire UpdateTokensAndSavings into makeProxyHandler after upstream response"
```

---

### Task 5: Admin ModelService — UpdatePricing + prices in ModelConfig

**Files:**
- Modify: `admin/services/models.go`

- [ ] **Step 1: Write the failing test**

Add to a new file `admin/services/models_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestModelService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewModelService(nil)
	assert.NotNil(t, svc)
}

func TestModelConfig_PriceFields(t *testing.T) {
	input := 5.00
	output := 15.00
	m := services.ModelConfig{
		ID:              "m1",
		Name:            "gpt-4o",
		Provider:        "openai",
		SCURate:         1.0,
		IsActive:        true,
		PricePerMInput:  &input,
		PricePerMOutput: &output,
	}
	assert.Equal(t, "gpt-4o", m.Name)
	assert.Equal(t, 5.00, *m.PricePerMInput)
	assert.Equal(t, 15.00, *m.PricePerMOutput)
}

func TestModelConfig_NilPrices(t *testing.T) {
	m := services.ModelConfig{ID: "m2", Name: "gpt-4o-mini"}
	assert.Nil(t, m.PricePerMInput)
	assert.Nil(t, m.PricePerMOutput)
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestModelConfig -v
```
Expected: FAIL — `ModelConfig` has no `PricePerMInput` or `PricePerMOutput` fields.

- [ ] **Step 3: Update admin/services/models.go**

Replace the full file content:

```go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelConfig struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	BaseURL         string   `json:"base_url"`
	SCURate         float64  `json:"scu_rate"`
	IsActive        bool     `json:"is_active"`
	PricePerMInput  *float64 `json:"price_per_m_input"`
	PricePerMOutput *float64 `json:"price_per_m_output"`
}

type CreateModelRequest struct {
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	APIKey   string  `json:"api_key"`
	SCURate  float64 `json:"scu_rate"`
}

type ModelService struct {
	pool *pgxpool.Pool
}

func NewModelService(pool *pgxpool.Pool) *ModelService {
	return &ModelService{pool: pool}
}

func (s *ModelService) List(ctx context.Context, tenantID string) ([]*ModelConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, provider, base_url, scu_rate, is_active, price_per_m_input, price_per_m_output
		 FROM model_configs WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []*ModelConfig
	for rows.Next() {
		m := &ModelConfig{}
		if err := rows.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive,
			&m.PricePerMInput, &m.PricePerMOutput); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

func (s *ModelService) Create(ctx context.Context, tenantID string, req CreateModelRequest) (*ModelConfig, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Provider, req.APIKey, req.BaseURL, req.SCURate,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create model config: %w", err)
	}
	return &ModelConfig{ID: id, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseURL, SCURate: req.SCURate, IsActive: true}, nil
}

// UpdatePricing sets per-million-token pricing for a model config.
func (s *ModelService) UpdatePricing(ctx context.Context, tenantID, modelID string, inputPrice, outputPrice float64) (*ModelConfig, error) {
	var m ModelConfig
	err := s.pool.QueryRow(ctx,
		`UPDATE model_configs
		 SET price_per_m_input=$1, price_per_m_output=$2
		 WHERE id=$3 AND tenant_id=$4
		 RETURNING id, name, provider, base_url, scu_rate, is_active, price_per_m_input, price_per_m_output`,
		inputPrice, outputPrice, modelID, tenantID,
	).Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive, &m.PricePerMInput, &m.PricePerMOutput)
	if err != nil {
		return nil, fmt.Errorf("update model pricing: %w", err)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestModelConfig -v
```
Expected: all PASS.

- [ ] **Step 5: Verify admin compiles**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add admin/services/models.go admin/services/models_test.go
git commit -m "feat(admin/services): ModelConfig price fields + ModelService.UpdatePricing"
```

---

### Task 6: Admin MonthlySavingsReport upgrade — TotalUSDSaved + AvgComplexityScore

**Files:**
- Modify: `admin/services/cost_savings_report.go`
- Modify: `admin/services/cost_savings_report_test.go`

- [ ] **Step 1: Write the failing tests**

Replace `admin/services/cost_savings_report_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestMonthlySavingsReport_ServiceCreation(t *testing.T) {
	svc := services.NewCostSavingsReportService(nil)
	assert.NotNil(t, svc)
}

func TestMonthlySavingsReport_NewFields(t *testing.T) {
	usd := 42.50
	avg := 38.0
	r := services.MonthlySavingsReport{
		YearMonth:          "2026-05",
		RoutingEventCount:  100,
		TotalUSDSaved:      &usd,
		AvgComplexityScore: &avg,
		RoutingEventModels: []services.RoutedModelStat{},
		GeneratedAt:        "2026-05-15T00:00:00Z",
	}
	assert.Equal(t, 42.50, *r.TotalUSDSaved)
	assert.Equal(t, 38.0, *r.AvgComplexityScore)
}

func TestMonthlySavingsReport_NilPricesAllowed(t *testing.T) {
	r := services.MonthlySavingsReport{
		YearMonth:     "2026-05",
		GeneratedAt:   "2026-05-15T00:00:00Z",
	}
	assert.Nil(t, r.TotalUSDSaved)
	assert.Nil(t, r.AvgComplexityScore)
}

func TestRoutedModelStat_Fields(t *testing.T) {
	s := services.RoutedModelStat{OriginalModel: "claude-opus-4-7", RoutedModel: "claude-sonnet-4-6", Count: 42}
	assert.Equal(t, "claude-opus-4-7", s.OriginalModel)
	assert.Equal(t, int64(42), s.Count)
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestMonthlySavings -v
```
Expected: FAIL — `MonthlySavingsReport` has no `TotalUSDSaved` or `AvgComplexityScore` fields.

- [ ] **Step 3: Update admin/services/cost_savings_report.go**

Replace the full file:

```go
package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MonthlySavingsReport struct {
	YearMonth          string            `json:"year_month"`
	RoutingEventCount  int64             `json:"routing_event_count"`
	TotalUSDSaved      *float64          `json:"total_usd_saved"`      // nil when no pricing data
	AvgComplexityScore *float64          `json:"avg_complexity_score"` // nil when no events
	RoutingEventModels []RoutedModelStat `json:"routed_models"`
	GeneratedAt        string            `json:"generated_at"`
}

type RoutedModelStat struct {
	OriginalModel string `json:"original_model"`
	RoutedModel   string `json:"routed_model"`
	Count         int64  `json:"count"`
}

type CostSavingsReportService struct{ pool *pgxpool.Pool }

func NewCostSavingsReportService(pool *pgxpool.Pool) *CostSavingsReportService {
	return &CostSavingsReportService{pool: pool}
}

func (s *CostSavingsReportService) GetMonthlySavings(ctx context.Context, tenantID, yearMonth string) (*MonthlySavingsReport, error) {
	report := &MonthlySavingsReport{
		YearMonth:   yearMonth,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*), SUM(usd_saved), AVG(complexity_score::float)
		 FROM gateway_routing_events
		 WHERE tenant_id=$1 AND to_char(routed_at AT TIME ZONE 'UTC','YYYY-MM')=$2`,
		tenantID, yearMonth,
	).Scan(&report.RoutingEventCount, &report.TotalUSDSaved, &report.AvgComplexityScore)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT original_model, routed_model, COUNT(*) AS cnt
		 FROM gateway_routing_events
		 WHERE tenant_id=$1 AND to_char(routed_at AT TIME ZONE 'UTC','YYYY-MM')=$2
		 GROUP BY original_model, routed_model ORDER BY cnt DESC`,
		tenantID, yearMonth)
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

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestMonthlySavings -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/cost_savings_report.go admin/services/cost_savings_report_test.go
git commit -m "feat(admin/services): MonthlySavingsReport — add TotalUSDSaved + AvgComplexityScore"
```

---

### Task 7: Admin API — PUT /models/:id/pricing + GET /cost/savings

**Files:**
- Modify: `admin/api/models.go`
- Modify: `admin/api/models_test.go`
- Modify: `admin/api/cost_savings.go`
- Modify: `admin/api/cost_savings_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `admin/api/models_test.go` (create the file if it doesn't exist):

```go
package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubModelSvc struct{}

func (s *stubModelSvc) List(_ context.Context, _ string) ([]*services.ModelConfig, error) {
	return []*services.ModelConfig{}, nil
}
func (s *stubModelSvc) Create(_ context.Context, _ string, _ services.CreateModelRequest) (*services.ModelConfig, error) {
	return &services.ModelConfig{ID: "m1", Name: "gpt-4o"}, nil
}
func (s *stubModelSvc) UpdatePricing(_ context.Context, _, _ string, input, output float64) (*services.ModelConfig, error) {
	p := input
	q := output
	return &services.ModelConfig{ID: "m1", Name: "gpt-4o", PricePerMInput: &p, PricePerMOutput: &q}, nil
}

func setupModelsApp(svc *stubModelSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterModelRoutes(app, svc)
	return app
}

func TestUpdateModelPricing_OK(t *testing.T) {
	app := setupModelsApp(&stubModelSvc{})
	body := `{"price_per_m_input":5.00,"price_per_m_output":15.00}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/models/m1/pricing", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestUpdateModelPricing_MissingFields(t *testing.T) {
	app := setupModelsApp(&stubModelSvc{})
	body := `{"price_per_m_input":5.00}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/models/m1/pricing", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}
```

Update `admin/api/cost_savings_test.go` stub to include new fields:

```go
// Replace stubSavingsSvc.GetMonthlySavings:
func (s *stubSavingsSvc) GetMonthlySavings(_ context.Context, _, _ string) (*services.MonthlySavingsReport, error) {
	usd := 42.50
	avg := 38.0
	return &services.MonthlySavingsReport{
		YearMonth:          "2026-05",
		RoutingEventCount:  10,
		TotalUSDSaved:      &usd,
		AvgComplexityScore: &avg,
		RoutingEventModels: []services.RoutedModelStat{},
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run "TestUpdateModelPricing|TestGetCostSavings" -v
```
Expected: FAIL — `ModelServiceInterface` missing `UpdatePricing`, `stubModelSvc` compile errors.

- [ ] **Step 3: Update admin/api/models.go — add UpdatePricing method + PUT endpoint**

Replace the full `admin/api/models.go`:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ModelServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error)
	Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error)
	UpdatePricing(ctx context.Context, tenantID, modelID string, inputPrice, outputPrice float64) (*services.ModelConfig, error)
}

func RegisterModelRoutes(app fiber.Router, svc ModelServiceInterface) {
	app.Get("/api/models", listModels(svc))
	app.Post("/api/models", createModel(svc))
	app.Put("/api/admin/models/:id/pricing", updateModelPricing(svc))
}

func listModels(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		models, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(models), "models": models})
	}
}

func createModel(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.CreateModelRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Name == "" || req.Provider == "" || req.BaseURL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name, provider, base_url required"})
		}
		model, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(model)
	}
}

func updateModelPricing(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			PricePerMInput  *float64 `json:"price_per_m_input"`
			PricePerMOutput *float64 `json:"price_per_m_output"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.PricePerMInput == nil || body.PricePerMOutput == nil {
			return c.Status(400).JSON(fiber.Map{"error": "price_per_m_input and price_per_m_output are required"})
		}
		m, err := svc.UpdatePricing(c.Context(), claims.TenantID, c.Params("id"), *body.PricePerMInput, *body.PricePerMOutput)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(m)
	}
}
```

- [ ] **Step 4: Add GET /cost/savings endpoint to cost_savings.go**

Add to `admin/api/cost_savings.go`, inside `RegisterCostSavingsRoutes`:

```go
func RegisterCostSavingsRoutes(app fiber.Router, svc CostSavingsServiceIface) {
	app.Get("/api/admin/cost/savings-report", getCostSavingsReport(svc)) // existing
	app.Get("/api/admin/cost/savings", getCostSavingsReport(svc))        // new alias with new fields
}
```

(Both routes call the same handler; the service now returns the enriched struct.)

- [ ] **Step 5: Run tests — verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/... -run "TestUpdateModelPricing|TestGetCostSavings" -v
```
Expected: all PASS.

- [ ] **Step 6: Run all admin tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./...
```
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add admin/api/models.go admin/api/models_test.go admin/api/cost_savings.go admin/api/cost_savings_test.go
git commit -m "feat(admin/api): PUT /models/:id/pricing + GET /cost/savings with USD+complexity fields"
```

---

### Task 8: Dashboard — CostCenterPage stat cards + ModelsPage pricing form

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Modify: `dashboard/src/pages/admin/CostCenterPage.tsx`
- Modify: `dashboard/src/pages/admin/ModelsPage.tsx`

- [ ] **Step 1: Add updateModelPricing to client.ts**

In `dashboard/src/api/client.ts`, after the `createModel` export (search for `export const createModel`), add:

```typescript
export const updateModelPricing = async (
  id: string,
  pricing: { price_per_m_input: number; price_per_m_output: number }
): Promise<any> => {
  const { data } = await apiClient.put(`/api/admin/models/${id}/pricing`, pricing);
  return data;
};
```

- [ ] **Step 2: Update CostCenterPage.tsx — expand savings query type**

In `dashboard/src/pages/admin/CostCenterPage.tsx`, find the `savings` query (line ~275) and update the generic type argument:

```typescript
const { data: savings } = useQuery({
  queryKey: ["cost-savings", month],
  queryFn: () =>
    apiClient
      .get<{
        routing_event_count: number;
        total_usd_saved: number | null;
        avg_complexity_score: number | null;
        routed_models: { original_model: string; routed_model: string; count: number }[];
        generated_at: string;
      }>(`/api/admin/cost/savings-report?month=${month}`)
      .then((r) => r.data),
});
```

- [ ] **Step 3: Add stat cards to CostCenterPage.tsx**

Find the `/* Auto-Routing Savings */` section (around line 464). After the existing green banner (`<div className="mb-4 px-4 py-3 bg-green-50 ...">...</div>`), add two new stat cards:

```tsx
{/* New stat cards for USD savings + complexity */}
<div className="grid grid-cols-2 gap-4 mb-4">
  <div className="px-4 py-3 bg-blue-50 border border-blue-100 rounded-lg">
    <p className="text-sm text-gray-500">本月节省</p>
    <p className="text-2xl font-bold text-blue-600">
      {savings?.total_usd_saved != null
        ? `$${savings.total_usd_saved.toFixed(2)}`
        : <span className="text-gray-400">—</span>}
    </p>
    {savings?.total_usd_saved == null && (
      <p className="text-xs text-gray-400 mt-1">请在模型配置中填写价格</p>
    )}
  </div>
  <div className="px-4 py-3 bg-purple-50 border border-purple-100 rounded-lg">
    <p className="text-sm text-gray-500">平均复杂度</p>
    <p className="text-2xl font-bold text-purple-600">
      {savings?.avg_complexity_score != null
        ? `${savings.avg_complexity_score.toFixed(0)} / 100`
        : <span className="text-gray-400">—</span>}
    </p>
  </div>
</div>
```

- [ ] **Step 4: Add pricing form to ModelsPage.tsx**

In `dashboard/src/pages/admin/ModelsPage.tsx`, add the import and state:

```typescript
import { updateModelPricing } from "../../api/client";
```

Add state for the pricing dialog (after the existing `useState` calls):
```typescript
const [pricingModelId, setPricingModelId] = useState<string | null>(null);
const [inputPrice, setInputPrice] = useState("");
const [outputPrice, setOutputPrice] = useState("");
```

Add the mutation:
```typescript
const pricingMutation = useMutation({
  mutationFn: ({ id, input, output }: { id: string; input: number; output: number }) =>
    updateModelPricing(id, { price_per_m_input: input, price_per_m_output: output }),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ["models"] });
    setPricingModelId(null);
    setInputPrice(""); setOutputPrice("");
  },
});
```

In the model list table/cards, add a "Set Price" button per model row. Find the `{data?.models?.map((m) => (` section and add a button inside each model card:

```tsx
<button
  onClick={() => { setPricingModelId(m.id); setInputPrice(m.price_per_m_input?.toString() ?? ""); setOutputPrice(m.price_per_m_output?.toString() ?? ""); }}
  className="text-xs text-blue-600 hover:underline"
>
  {m.price_per_m_input != null ? `$${m.price_per_m_input}/M` : "Set Price"}
</button>
```

Add the pricing dialog (before the closing `</div>` of the page):

```tsx
{pricingModelId && (
  <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
    <div className="bg-white rounded-xl p-6 space-y-4 w-80">
      <h3 className="font-semibold">Set Model Pricing (per 1M tokens)</h3>
      <div className="space-y-2">
        <label className="block text-sm">
          Input Price ($)
          <input
            type="number" step="0.01" value={inputPrice}
            onChange={(e) => setInputPrice(e.target.value)}
            className="block w-full border rounded px-3 py-2 mt-1"
            placeholder="e.g. 5.00"
          />
        </label>
        <label className="block text-sm">
          Output Price ($)
          <input
            type="number" step="0.01" value={outputPrice}
            onChange={(e) => setOutputPrice(e.target.value)}
            className="block w-full border rounded px-3 py-2 mt-1"
            placeholder="e.g. 15.00"
          />
        </label>
      </div>
      <div className="flex gap-2 justify-end">
        <button onClick={() => setPricingModelId(null)} className="px-4 py-2 border rounded">Cancel</button>
        <button
          disabled={pricingMutation.isPending}
          onClick={() => pricingMutation.mutate({ id: pricingModelId, input: parseFloat(inputPrice), output: parseFloat(outputPrice) })}
          className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
        >
          Save
        </button>
      </div>
    </div>
  </div>
)}
```

- [ ] **Step 5: Run TypeScript type-check**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit 2>&1 | tail -20
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/api/client.ts dashboard/src/pages/admin/CostCenterPage.tsx dashboard/src/pages/admin/ModelsPage.tsx
git commit -m "feat(dashboard): cost savings stat cards (本月节省/平均复杂度) + model pricing form"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** Migration 023 ✓ | `complexityScore` (5 signals: body 40, messages 20, system 20, toolCalls 10, keywords 10) ✓ | `ROUTER_COMPLEXITY_THRESHOLD` env default 50 ✓ | `RoutingEvent.ComplexityScore` ✓ | `RoutingStore.Record` returns `(int64, error)` synchronously ✓ | `routing_event_id` stored in `c.Locals` ✓ | `UpdateTokensAndSavings` (NULL when prices absent, formula: tokens/1M × price_diff) ✓ | `ModelPrice` struct ✓ | Admin `ModelConfig` price fields ✓ | `ModelService.UpdatePricing` ✓ | `MonthlySavingsReport.TotalUSDSaved + AvgComplexityScore` ✓ | `PUT /api/admin/models/:id/pricing` ✓ | `GET /api/admin/cost/savings` ✓ | Dashboard stat cards ✓ | Dashboard pricing form ✓
- [x] **Type consistency:** `ModelPrice` defined in `gateway/storage/routing_store.go`, used in same package's `UpdateTokensAndSavings` and `gateway/main.go` via `storage.ModelPrice` | `MonthlySavingsReport` defined in `admin/services/cost_savings_report.go`, tested in `cost_savings_report_test.go`, returned by API in `cost_savings.go` | `ModelServiceInterface.UpdatePricing` signature matches `ModelService.UpdatePricing` and `stubModelSvc.UpdatePricing`
- [x] **Existing test updates:** `captureRecorder.Record` updated to `(int64, error)` in Task 2 | `TestAutoRouter_PremiumModel_LongBody_NotDowngraded` renamed and updated to high-complexity body in Task 2 | `cost_savings_test.go` stub updated in Task 7
- [x] **No placeholders:** All steps have complete code
