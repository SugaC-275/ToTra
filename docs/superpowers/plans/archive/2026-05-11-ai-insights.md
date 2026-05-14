# AI KPI Insights Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an AI-generated insight summary to each employee's KPI view — a 3–4 sentence analysis of their AIQ/OSS/GTS scores, calling the Anthropic Messages API directly from the admin service backend.

**Architecture:** New `AIInsightsService` fetches the user's efficiency snapshot from the DB, builds a prompt, and calls `https://api.anthropic.com/v1/messages` using the `ANTHROPIC_API_KEY` env var. If the key is absent or the snapshot is missing, a graceful fallback string is returned. New API endpoint `GET /api/me/kpi/insights?month=YYYY-MM` is employee-only (any role). The frontend `MyUsagePage.tsx` gains an "AI Insights" card.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, `net/http` for outbound Anthropic call, `encoding/json`; React 19 + TypeScript, TanStack Query v5.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `admin/services/ai_insights.go` (create), `admin/services/ai_insights_test.go` (create) |
| 1 | `admin/api/ai_insights.go` (create), `admin/api/ai_insights_test.go` (create), `admin/main.go` (modify) |
| 2 | `dashboard/src/api/client.ts` (modify), `dashboard/src/pages/employee/MyUsagePage.tsx` (modify) |

---

## Task 0: Service layer — AIInsightsService

**Files:**
- Create: `admin/services/ai_insights.go`
- Create: `admin/services/ai_insights_test.go`

Read `admin/services/bot.go` for HTTP client pattern reference. Read `admin/services/kpi.go` to understand what columns are in `efficiency_snapshots`.

- [ ] **Step 1: Create `admin/services/ai_insights_test.go`**

```go
package services_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

func TestBuildKPIPrompt(t *testing.T) {
	snap := services.KPISnapshotForInsight{
		UserName:        "Alice",
		Month:           "2026-05",
		AIQScore:        85.5,
		OSSScore:        72.0,
		GTSScore:        90.0,
		EfficiencyScore: 82.5,
		AnomalyFlagged:  false,
	}
	prompt := services.BuildKPIPrompt(snap)
	assert.Contains(t, prompt, "Alice")
	assert.Contains(t, prompt, "2026-05")
	assert.Contains(t, prompt, "85.5")
	assert.Contains(t, prompt, "AIQ")
}

func TestCallAnthropicAPI_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Great performance this month!"},
			},
		})
	}))
	defer srv.Close()

	result, err := services.CallAnthropicAPI(srv.URL, "test-key", "Analyze this KPI data")
	require.NoError(t, err)
	assert.Equal(t, "Great performance this month!", result)
}

func TestCallAnthropicAPI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := services.CallAnthropicAPI(srv.URL, "bad-key", "prompt")
	assert.Error(t, err)
}

func TestCallAnthropicAPI_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{},
		})
	}))
	defer srv.Close()

	_, err := services.CallAnthropicAPI(srv.URL, "key", "prompt")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestBuildKPIPrompt|TestCallAnthropicAPI" -v 2>&1 | head -20
```

Expected: compilation error.

- [ ] **Step 3: Create `admin/services/ai_insights.go`**

```go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

var insightHTTPClient = &http.Client{Timeout: 30 * time.Second}

type KPISnapshotForInsight struct {
	UserName        string
	Month           string
	AIQScore        float64
	OSSScore        float64
	GTSScore        float64
	EfficiencyScore float64
	AnomalyFlagged  bool
}

type AIInsightsService struct {
	pool   *pgxpool.Pool
	apiKey string
}

func NewAIInsightsService(pool *pgxpool.Pool, apiKey string) *AIInsightsService {
	return &AIInsightsService{pool: pool, apiKey: apiKey}
}

func (s *AIInsightsService) GenerateInsight(ctx context.Context, userID, month string) (string, error) {
	if s.apiKey == "" {
		return "AI insights are not configured. Please set ANTHROPIC_API_KEY.", nil
	}

	var snap KPISnapshotForInsight
	snap.Month = month
	err := s.pool.QueryRow(ctx,
		`SELECT u.name, es.aiq_score, es.oss_score, es.gts_score,
		        es.efficiency_score, es.anomaly_flagged
		 FROM efficiency_snapshots es
		 JOIN users u ON u.id = es.user_id
		 WHERE es.user_id = $1 AND es.year_month = $2`,
		userID, month,
	).Scan(&snap.UserName, &snap.AIQScore, &snap.OSSScore, &snap.GTSScore,
		&snap.EfficiencyScore, &snap.AnomalyFlagged)
	if err != nil {
		return "No KPI data available for this month.", nil
	}

	if snap.AnomalyFlagged {
		return fmt.Sprintf("An anomaly was detected in %s's usage for %s. KPI scores have been zeroed out pending review.", snap.UserName, month), nil
	}

	prompt := BuildKPIPrompt(snap)
	return CallAnthropicAPI(anthropicAPIURL, s.apiKey, prompt)
}

// BuildKPIPrompt creates the Anthropic prompt from a KPI snapshot.
func BuildKPIPrompt(snap KPISnapshotForInsight) string {
	return fmt.Sprintf(
		`You are an AI performance coach. Analyze the following KPI scores for %s in %s and provide a concise 3-4 sentence insight summary. Focus on strengths and one specific area for improvement. Be encouraging but honest.

AIQ (AI Usage Quality): %.1f/100
OSS (Open Source Contribution): %.1f/100
GTS (Goal & Task Completion): %.1f/100
Overall Efficiency Score: %.1f/100

Provide the insight in plain text, no markdown formatting.`,
		snap.UserName, snap.Month,
		snap.AIQScore, snap.OSSScore, snap.GTSScore, snap.EfficiencyScore,
	)
}

// CallAnthropicAPI sends a prompt to the Anthropic Messages API and returns the response text.
// The apiURL parameter allows injection of a test server URL.
func CallAnthropicAPI(apiURL, apiKey, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := insightHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic API")
	}
	return result.Content[0].Text, nil
}
```

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestBuildKPIPrompt|TestCallAnthropicAPI" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Run all service tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -v 2>&1 | tail -10
```

- [ ] **Step 6: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/services/ai_insights.go admin/services/ai_insights_test.go
git commit -m "feat(services): add AIInsightsService for KPI insight generation"
```

---

## Task 1: API route + main.go wiring

**Files:**
- Create: `admin/api/ai_insights.go`
- Create: `admin/api/ai_insights_test.go`
- Modify: `admin/main.go`

Read `admin/api/kpi.go` to understand how the existing KPI routes are structured.

- [ ] **Step 1: Create `admin/api/ai_insights_test.go`**

```go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubInsightSvc struct {
	insight string
	err     error
}

func (s *stubInsightSvc) GenerateInsight(_ context.Context, _, _ string) (string, error) {
	return s.insight, s.err
}

func setupInsightApp(svc *stubInsightSvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterAIInsightsRoutes(app, svc)
	return app
}

func TestGetKPIInsights_OK(t *testing.T) {
	app := setupInsightApp(&stubInsightSvc{insight: "Great work this month!"})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "Great work this month!", result["insight"])
	assert.Equal(t, "2026-05", result["month"])
}

func TestGetKPIInsights_DefaultMonth(t *testing.T) {
	app := setupInsightApp(&stubInsightSvc{insight: "AI insight text"})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result["month"])
}

func TestGetKPIInsights_Unauthenticated(t *testing.T) {
	app := fiber.New()
	api.RegisterAIInsightsRoutes(app, &stubInsightSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/me/kpi/insights", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGetKPIInsights" -v 2>&1 | head -20
```

Expected: compilation error.

- [ ] **Step 3: Create `admin/api/ai_insights.go`**

```go
package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type AIInsightsServiceIface interface {
	GenerateInsight(ctx context.Context, userID, month string) (string, error)
}

func RegisterAIInsightsRoutes(app fiber.Router, svc AIInsightsServiceIface) {
	app.Get("/api/me/kpi/insights", getKPIInsights(svc))
}

func getKPIInsights(svc AIInsightsServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.UserID == "" {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		month := c.Query("month")
		if month == "" {
			month = time.Now().UTC().Format("2006-01")
		}
		insight, err := svc.GenerateInsight(c.Context(), claims.UserID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"insight": insight, "month": month})
	}
}
```

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGetKPIInsights" -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Update `admin/main.go`**

Read the file. Find where `botSvc` is initialized. Add after it:

```go
insightsSvc := services.NewAIInsightsService(pool, os.Getenv("ANTHROPIC_API_KEY"))
```

Find where `api.RegisterBotRoutes(protected, botSvc)` is called. Add after it:

```go
api.RegisterAIInsightsRoutes(protected, insightsSvc)
```

Verify `os` is already imported (it should be for the existing `os.Getenv` calls).

- [ ] **Step 6: Run all API tests and build**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -v 2>&1 | tail -10
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/api/ai_insights.go admin/api/ai_insights_test.go admin/main.go
git commit -m "feat(api): add GET /api/me/kpi/insights endpoint for AI-generated KPI summaries"
```

---

## Task 2: Frontend — AI Insights card on MyUsagePage

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Modify: `dashboard/src/pages/employee/MyUsagePage.tsx`

Read both files first to understand their current structure before editing.

- [ ] **Step 1: Add `getMyKPIInsights` to `dashboard/src/api/client.ts`**

After the existing KPI/submetrics functions, add:

```typescript
export const getMyKPIInsights = async (month: string): Promise<{ insight: string; month: string }> => {
  const { data } = await api.get(`/api/me/kpi/insights?month=${month}`);
  return data;
};
```

- [ ] **Step 2: Add AI Insights card to `dashboard/src/pages/employee/MyUsagePage.tsx`**

Read the file. Find a suitable location to add an AI Insights card — after the existing KPI score cards or submetrics section.

Add an import for `getMyKPIInsights` from `../../api/client`.

Add a new `useQuery` hook:

```typescript
const { data: insightsData, isLoading: insightsLoading } = useQuery({
  queryKey: ["kpi-insights", month],
  queryFn: () => getMyKPIInsights(month),
  retry: false,
});
```

Where `month` is whatever the existing month variable is in the component (check the existing code).

Add a card after existing cards:

```tsx
{/* AI Insights */}
<div className="bg-gradient-to-br from-purple-50 to-blue-50 rounded-lg border border-purple-200 p-4">
  <h3 className="font-semibold text-purple-800 mb-2">AI Insights</h3>
  {insightsLoading ? (
    <p className="text-gray-500 text-sm">Generating insights...</p>
  ) : insightsData?.insight ? (
    <p className="text-gray-700 text-sm leading-relaxed">{insightsData.insight}</p>
  ) : (
    <p className="text-gray-400 text-sm">No insights available for this month.</p>
  )}
</div>
```

- [ ] **Step 3: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/api/client.ts dashboard/src/pages/employee/MyUsagePage.tsx
git commit -m "feat(dashboard): add AI Insights card to employee KPI page"
```
