# Bot Notification Push Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow tenant admins to configure Feishu/Slack outgoing bot webhook URLs and send monthly KPI summary notifications to those bots, plus send a test notification to verify the configuration works.

**Architecture:** New `tenant_bot_configs` table stores AES-256-GCM encrypted webhook URLs per tenant. A `BotService` handles CRUD and outbound HTTP POSTs. Four admin-only API routes expose list/add/delete/send-summary. A React `BotConfigPage` lets admins manage bots and send summaries.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, `admin/crypto` (AES-256-GCM), `net/http` (outbound); React 19 + TypeScript, TanStack Query v5.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `infra/postgres/008_bot_configs.sql` (create) |
| 1 | `admin/services/bot.go` (create), `admin/services/bot_test.go` (create) |
| 2 | `admin/api/bot.go` (create), `admin/api/bot_test.go` (create) |
| 3 | `admin/main.go` (modify), `dashboard/src/api/client.ts` (modify), `dashboard/src/pages/admin/BotConfigPage.tsx` (create), `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 0: DB migration — tenant_bot_configs table

**Files:**
- Create: `infra/postgres/008_bot_configs.sql`

- [ ] **Step 1: Create migration file**

```sql
BEGIN;

CREATE TABLE IF NOT EXISTS tenant_bot_configs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    platform        TEXT        NOT NULL CHECK (platform IN ('feishu', 'slack')),
    encrypted_url   TEXT        NOT NULL,
    label           TEXT        NOT NULL DEFAULT '',
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform, label)
);

CREATE INDEX IF NOT EXISTS idx_tenant_bot_configs_tenant_id
    ON tenant_bot_configs (tenant_id);

COMMIT;
```

- [ ] **Step 2: Apply migration**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra < /Users/sugac.275/ToTra/infra/postgres/008_bot_configs.sql
```

Expected output: BEGIN, CREATE TABLE, CREATE INDEX, COMMIT.

- [ ] **Step 3: Verify table**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra -c "\d tenant_bot_configs"
```

Expected: table with columns id, tenant_id, platform, encrypted_url, label, enabled, created_at.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add infra/postgres/008_bot_configs.sql
git commit -m "feat(db): add tenant_bot_configs table for bot notification webhooks"
```

---

## Task 1: Service layer — BotService

**Files:**
- Create: `admin/services/bot.go`
- Create: `admin/services/bot_test.go`

Read `admin/services/webhook.go` and `admin/services/ip_allowlist.go` first to understand existing patterns for encrypted-secret storage and service structure.

- [ ] **Step 1: Write failing tests in `admin/services/bot_test.go`**

```go
package services_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

const testBotEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestFormatBotMessage(t *testing.T) {
	msg := services.FormatKPISummaryMessage("Acme Corp", "2026-05", []services.BotTopEntry{
		{UserName: "Alice", AIQScore: 88.5},
		{UserName: "Bob", AIQScore: 75.0},
	})
	assert.Contains(t, msg, "Acme Corp")
	assert.Contains(t, msg, "2026-05")
	assert.Contains(t, msg, "Alice")
	assert.Contains(t, msg, "88.5")
}

func TestSendBotMessage_Feishu(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	err := services.SendBotMessage("feishu", srv.URL, "hello feishu")
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(bytes.NewReader(received)).Decode(&payload))
	assert.Equal(t, "text", payload["msg_type"])
	content, _ := payload["content"].(map[string]interface{})
	assert.Equal(t, "hello feishu", content["text"])
}

func TestSendBotMessage_Slack(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := services.SendBotMessage("slack", srv.URL, "hello slack")
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(bytes.NewReader(received)).Decode(&payload))
	assert.Equal(t, "hello slack", payload["text"])
}

func TestSendBotMessage_UnknownPlatform(t *testing.T) {
	err := services.SendBotMessage("discord", "http://example.com", "msg")
	assert.Error(t, err)
}

func TestSendBotMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := services.SendBotMessage("slack", srv.URL, "msg")
	assert.Error(t, err)
}

func TestEncryptDecryptBotURL(t *testing.T) {
	url := "https://open.feishu.cn/open-apis/bot/v2/hook/abc123"
	enc, err := crypto.Encrypt(url, testBotEncKey)
	require.NoError(t, err)
	assert.NotEqual(t, url, enc)

	dec, err := crypto.Decrypt(enc, testBotEncKey)
	require.NoError(t, err)
	assert.Equal(t, url, dec)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestFormatBotMessage|TestSendBotMessage|TestEncryptDecryptBotURL" -v 2>&1 | head -20
```

Expected: compilation error — functions undefined.

- [ ] **Step 3: Create `admin/services/bot.go`**

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
	"github.com/yourorg/totra/admin/crypto"
)

type BotConfig struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Platform  string    `json:"platform"`
	Label     string    `json:"label"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type BotTopEntry struct {
	UserName string
	AIQScore float64
}

type BotService struct {
	pool   *pgxpool.Pool
	encKey string
}

func NewBotService(pool *pgxpool.Pool, encKey string) *BotService {
	return &BotService{pool: pool, encKey: encKey}
}

func (s *BotService) List(ctx context.Context, tenantID string) ([]BotConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, platform, label, enabled, created_at
		 FROM tenant_bot_configs WHERE tenant_id = $1 ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []BotConfig
	for rows.Next() {
		var c BotConfig
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Platform, &c.Label, &c.Enabled, &c.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (s *BotService) Add(ctx context.Context, tenantID, platform, webhookURL, label string) (*BotConfig, error) {
	encURL, err := crypto.Encrypt(webhookURL, s.encKey)
	if err != nil {
		return nil, err
	}
	var c BotConfig
	err = s.pool.QueryRow(ctx,
		`INSERT INTO tenant_bot_configs (tenant_id, platform, encrypted_url, label)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, tenant_id, platform, label, enabled, created_at`,
		tenantID, platform, encURL, label,
	).Scan(&c.ID, &c.TenantID, &c.Platform, &c.Label, &c.Enabled, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *BotService) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tenant_bot_configs WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("bot config not found")
	}
	return nil
}

func (s *BotService) loadEnabledConfigs(ctx context.Context, tenantID string) ([]struct {
	platform     string
	webhookURL   string
}, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT platform, encrypted_url FROM tenant_bot_configs
		 WHERE tenant_id = $1 AND enabled = TRUE`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []struct {
		platform   string
		webhookURL string
	}
	for rows.Next() {
		var platform, encURL string
		if err := rows.Scan(&platform, &encURL); err != nil {
			return nil, err
		}
		url, err := crypto.Decrypt(encURL, s.encKey)
		if err != nil {
			continue
		}
		result = append(result, struct {
			platform   string
			webhookURL string
		}{platform, url})
	}
	return result, rows.Err()
}

func (s *BotService) SendKPISummary(ctx context.Context, tenantID, month string) error {
	configs, err := s.loadEnabledConfigs(ctx, tenantID)
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}

	// Query top 3 by AIQ score for the month
	rows, err := s.pool.Query(ctx,
		`SELECT u.name, es.aiq_score
		 FROM efficiency_snapshots es
		 JOIN users u ON u.id = es.user_id
		 WHERE es.tenant_id = $1 AND es.year_month = $2
		   AND es.anomaly_flagged = FALSE
		 ORDER BY es.aiq_score DESC
		 LIMIT 3`,
		tenantID, month,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var topEntries []BotTopEntry
	for rows.Next() {
		var e BotTopEntry
		if err := rows.Scan(&e.UserName, &e.AIQScore); err != nil {
			return err
		}
		topEntries = append(topEntries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Get tenant name
	var tenantName string
	s.pool.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, tenantID).Scan(&tenantName)

	message := FormatKPISummaryMessage(tenantName, month, topEntries)

	var sendErr error
	for _, cfg := range configs {
		if err := SendBotMessage(cfg.platform, cfg.webhookURL, message); err != nil {
			sendErr = err
		}
	}
	return sendErr
}

func (s *BotService) SendTestMessage(ctx context.Context, tenantID, id string) error {
	var platform, encURL string
	err := s.pool.QueryRow(ctx,
		`SELECT platform, encrypted_url FROM tenant_bot_configs WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(&platform, &encURL)
	if err != nil {
		return fmt.Errorf("bot config not found")
	}
	url, err := crypto.Decrypt(encURL, s.encKey)
	if err != nil {
		return err
	}
	return SendBotMessage(platform, url, "✅ ToTra bot notification test — connection successful!")
}

// FormatKPISummaryMessage builds the notification text for KPI summary.
func FormatKPISummaryMessage(tenantName, month string, top []BotTopEntry) string {
	msg := fmt.Sprintf("📊 ToTra KPI Summary — %s (%s)\n", tenantName, month)
	if len(top) == 0 {
		msg += "\nNo KPI snapshots found for this month."
		return msg
	}
	msg += "\nTop performers by AIQ score:\n"
	for i, e := range top {
		msg += fmt.Sprintf("%d. %s — AIQ: %.1f\n", i+1, e.UserName, e.AIQScore)
	}
	return msg
}

// SendBotMessage posts a text message to a Feishu or Slack webhook URL.
func SendBotMessage(platform, webhookURL, message string) error {
	var body interface{}
	switch platform {
	case "feishu":
		body = map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": message},
		}
	case "slack":
		body = map[string]string{"text": message}
	default:
		return fmt.Errorf("unsupported bot platform: %s", platform)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("bot webhook returned status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestFormatBotMessage|TestSendBotMessage|TestEncryptDecryptBotURL" -v
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Run all service tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -v 2>&1 | tail -10
```

Expected: all tests pass.

- [ ] **Step 6: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/services/bot.go admin/services/bot_test.go
git commit -m "feat(services): add BotService for Feishu/Slack notification push"
```

---

## Task 2: API layer — bot routes

**Files:**
- Create: `admin/api/bot.go`
- Create: `admin/api/bot_test.go`

Read `admin/api/ip_allowlist.go` to understand the existing admin-only CRUD pattern before writing.

- [ ] **Step 1: Write failing tests in `admin/api/bot_test.go`**

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

// stubBotSvc implements api.BotServiceIface for testing.
type stubBotSvc struct {
	configs  []services.BotConfig
	addErr   error
	delErr   error
	sendErr  error
	testErr  error
}

func (s *stubBotSvc) List(_ context.Context, _ string) ([]services.BotConfig, error) {
	return s.configs, nil
}
func (s *stubBotSvc) Add(_ context.Context, _, _, _, _ string) (*services.BotConfig, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	c := &services.BotConfig{ID: "new-id", Platform: "feishu", Label: "test", Enabled: true, CreatedAt: time.Now()}
	return c, nil
}
func (s *stubBotSvc) Delete(_ context.Context, _, _ string) error { return s.delErr }
func (s *stubBotSvc) SendKPISummary(_ context.Context, _, _ string) error { return s.sendErr }
func (s *stubBotSvc) SendTestMessage(_ context.Context, _, _ string) error { return s.testErr }

func setupBotApp(svc *stubBotSvc) *fiber.App {
	app := fiber.New()
	// Inject fake admin claims
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterBotRoutes(app, svc)
	return app
}

func TestListBotConfigs_Empty(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/bot-configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	configs := result["configs"].([]interface{})
	assert.Len(t, configs, 0)
}

func TestAddBotConfig_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	body := `{"platform":"feishu","webhook_url":"https://open.feishu.cn/hook/abc","label":"main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestAddBotConfig_MissingFields(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	body := `{"platform":"feishu"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestDeleteBotConfig_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/bot-configs/some-id", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSendKPISummary_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs/send-summary?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestSendTestMessage_OK(t *testing.T) {
	app := setupBotApp(&stubBotSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bot-configs/some-id/test", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestBotRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterBotRoutes(app, &stubBotSvc{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bot-configs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestListBotConfigs|TestAddBotConfig|TestDeleteBotConfig|TestSendKPISummary|TestSendTestMessage|TestBotRoutes" -v 2>&1 | head -20
```

Expected: compilation error.

- [ ] **Step 3: Create `admin/api/bot.go`**

```go
package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type BotServiceIface interface {
	List(ctx context.Context, tenantID string) ([]services.BotConfig, error)
	Add(ctx context.Context, tenantID, platform, webhookURL, label string) (*services.BotConfig, error)
	Delete(ctx context.Context, tenantID, id string) error
	SendKPISummary(ctx context.Context, tenantID, month string) error
	SendTestMessage(ctx context.Context, tenantID, id string) error
}

func RegisterBotRoutes(app fiber.Router, svc BotServiceIface) {
	app.Get("/api/admin/bot-configs", listBotConfigs(svc))
	app.Post("/api/admin/bot-configs", addBotConfig(svc))
	app.Delete("/api/admin/bot-configs/:id", deleteBotConfig(svc))
	app.Post("/api/admin/bot-configs/send-summary", sendKPISummary(svc))
	app.Post("/api/admin/bot-configs/:id/test", sendTestMessage(svc))
}

func listBotConfigs(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		configs, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if configs == nil {
			configs = []services.BotConfig{}
		}
		return c.JSON(fiber.Map{"configs": configs})
	}
}

func addBotConfig(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Platform   string `json:"platform"`
			WebhookURL string `json:"webhook_url"`
			Label      string `json:"label"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Platform == "" || body.WebhookURL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and webhook_url are required"})
		}
		cfg, err := svc.Add(c.Context(), claims.TenantID, body.Platform, body.WebhookURL, body.Label)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(cfg)
	}
}

func deleteBotConfig(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

func sendKPISummary(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month")
		if month == "" {
			month = time.Now().UTC().Format("2006-01")
		}
		if err := svc.SendKPISummary(c.Context(), claims.TenantID, month); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "sent", "month": month})
	}
}

func sendTestMessage(svc BotServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.SendTestMessage(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}
```

**NOTE:** The test file's `stubBotSvc.SendTestMessage` must match this interface signature exactly. If the test uses `SendTestNotification` instead of `SendTestMessage`, rename accordingly.

- [ ] **Step 4: Run new tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestListBotConfigs|TestAddBotConfig|TestDeleteBotConfig|TestSendKPISummary|TestSendTestMessage|TestBotRoutes" -v
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Run all API tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -v 2>&1 | tail -10
```

Expected: all tests pass.

- [ ] **Step 6: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/api/bot.go admin/api/bot_test.go
git commit -m "feat(api): add bot notification CRUD and send-summary routes"
```

---

## Task 3: main.go wiring + frontend

**Files:**
- Modify: `admin/main.go`
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/BotConfigPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

Read all existing files before making changes. Follow established patterns from IPAllowlistPage.tsx for the frontend.

- [ ] **Step 1: Update `admin/main.go`**

Find where other services are initialized and routes are registered. Add after `ipAllowlistSvc`:

```go
botSvc := services.NewBotService(pool, os.Getenv("ENCRYPTION_KEY"))
```

Find where `api.RegisterIPAllowlistRoutes(protected, allowlistSvc)` is called. Add after it:

```go
api.RegisterBotRoutes(protected, botSvc)
```

- [ ] **Step 2: Build to confirm main.go compiles**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 3: Add types and API functions to `dashboard/src/api/client.ts`**

Add these types after the existing `IPAllowlistEntry` interface:

```typescript
export interface BotConfig {
  id: string;
  tenant_id: string;
  platform: string;
  label: string;
  enabled: boolean;
  created_at: string;
}
```

Add these functions after the IP allowlist functions:

```typescript
export const listBotConfigs = async (): Promise<{ configs: BotConfig[] }> => {
  const { data } = await api.get("/api/admin/bot-configs");
  return data;
};

export const addBotConfig = async (payload: {
  platform: string;
  webhook_url: string;
  label: string;
}): Promise<BotConfig> => {
  const { data } = await api.post("/api/admin/bot-configs", payload);
  return data;
};

export const deleteBotConfig = async (id: string): Promise<void> => {
  await api.delete(`/api/admin/bot-configs/${id}`);
};

export const sendKPISummary = async (month: string): Promise<void> => {
  await api.post(`/api/admin/bot-configs/send-summary?month=${month}`);
};

export const sendTestBotMessage = async (id: string): Promise<void> => {
  await api.post(`/api/admin/bot-configs/${id}/test`);
};
```

- [ ] **Step 4: Create `dashboard/src/pages/admin/BotConfigPage.tsx`**

```tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listBotConfigs,
  addBotConfig,
  deleteBotConfig,
  sendKPISummary,
  sendTestBotMessage,
  BotConfig,
} from "../../api/client";

export default function BotConfigPage() {
  const queryClient = useQueryClient();
  const [platform, setPlatform] = useState("feishu");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [label, setLabel] = useState("");
  const [addError, setAddError] = useState("");
  const [summaryMonth, setSummaryMonth] = useState(
    new Date().toISOString().slice(0, 7)
  );
  const [summaryMsg, setSummaryMsg] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["bot-configs"],
    queryFn: listBotConfigs,
  });

  const addMutation = useMutation({
    mutationFn: addBotConfig,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bot-configs"] });
      setWebhookUrl("");
      setLabel("");
      setAddError("");
    },
    onError: (err: any) => {
      setAddError(err?.response?.data?.error ?? "Failed to add bot config");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteBotConfig,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["bot-configs"] }),
  });

  const testMutation = useMutation({
    mutationFn: sendTestBotMessage,
  });

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault();
    if (!webhookUrl) return;
    addMutation.mutate({ platform, webhook_url: webhookUrl, label });
  };

  const handleSendSummary = async () => {
    try {
      await sendKPISummary(summaryMonth);
      setSummaryMsg("Summary sent successfully!");
    } catch {
      setSummaryMsg("Failed to send summary.");
    }
    setTimeout(() => setSummaryMsg(""), 3000);
  };

  const configs: BotConfig[] = data?.configs ?? [];

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Bot Notifications</h1>

      {/* Add bot config */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Add Bot Webhook</h2>
        <form onSubmit={handleAdd} className="space-y-3">
          <div className="flex gap-3">
            <select
              value={platform}
              onChange={(e) => setPlatform(e.target.value)}
              className="border rounded px-3 py-2"
            >
              <option value="feishu">飞书 (Feishu)</option>
              <option value="slack">Slack</option>
            </select>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="Label (optional)"
              className="border rounded px-3 py-2 w-48"
            />
          </div>
          <input
            type="url"
            value={webhookUrl}
            onChange={(e) => setWebhookUrl(e.target.value)}
            placeholder="Webhook URL"
            className="border rounded px-3 py-2 w-full"
            required
          />
          {addError && <p className="text-red-500 text-sm">{addError}</p>}
          <button
            type="submit"
            disabled={addMutation.isPending}
            className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {addMutation.isPending ? "Adding..." : "Add Webhook"}
          </button>
        </form>
      </div>

      {/* Configured bots */}
      <div className="bg-white rounded-lg border p-4">
        <h2 className="font-semibold text-lg mb-3">Configured Bots</h2>
        {isLoading ? (
          <p className="text-gray-500">Loading...</p>
        ) : configs.length === 0 ? (
          <p className="text-gray-400 text-sm">No bots configured yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-500 border-b">
                <th className="pb-2">Platform</th>
                <th className="pb-2">Label</th>
                <th className="pb-2">Enabled</th>
                <th className="pb-2">Added</th>
                <th className="pb-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {configs.map((c) => (
                <tr key={c.id} className="border-b last:border-0">
                  <td className="py-2 capitalize">{c.platform}</td>
                  <td className="py-2">{c.label || "—"}</td>
                  <td className="py-2">{c.enabled ? "✅" : "❌"}</td>
                  <td className="py-2">
                    {new Date(c.created_at).toLocaleDateString()}
                  </td>
                  <td className="py-2 space-x-2">
                    <button
                      onClick={() => testMutation.mutate(c.id)}
                      disabled={testMutation.isPending}
                      className="text-blue-600 hover:underline text-xs"
                    >
                      Test
                    </button>
                    <button
                      onClick={() => deleteMutation.mutate(c.id)}
                      disabled={deleteMutation.isPending}
                      className="text-red-500 hover:underline text-xs"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Send KPI summary */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Send KPI Summary</h2>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={summaryMonth}
            onChange={(e) => setSummaryMonth(e.target.value)}
            className="border rounded px-3 py-2"
          />
          <button
            onClick={handleSendSummary}
            className="bg-green-600 text-white px-4 py-2 rounded hover:bg-green-700"
          >
            Send Summary to All Bots
          </button>
        </div>
        {summaryMsg && <p className="text-sm text-gray-600">{summaryMsg}</p>}
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Update `dashboard/src/App.tsx`**

Find the existing admin routes section (where `/admin/ip-allowlist` is defined). Add after it:

```tsx
<Route path="admin/bot-configs" element={<ProtectedRoute adminOnly><BotConfigPage /></ProtectedRoute>} />
```

Add the import at the top with the other admin page imports:

```tsx
import BotConfigPage from "./pages/admin/BotConfigPage";
```

- [ ] **Step 6: Update `dashboard/src/components/Layout.tsx`**

Find where "IP Allowlist" navigation link is defined. Add after it:

```tsx
<NavLink to="/admin/bot-configs">Bot Notifications</NavLink>
```

(Use the exact same NavLink component/pattern as the surrounding links.)

- [ ] **Step 7: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/main.go dashboard/src/api/client.ts dashboard/src/pages/admin/BotConfigPage.tsx dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "feat: wire bot notification service and add BotConfigPage dashboard"
```
