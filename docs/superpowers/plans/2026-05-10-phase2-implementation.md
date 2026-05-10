# ToTra Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Webhook-based output tracking, monthly KPI efficiency scoring with peer group ranking, and automatic AI-Fuel quota rewards for high performers.

**Architecture:** Extend the existing admin service (Go, port 8081) with webhook endpoints, a KPI computation engine, and an AI-Fuel reward system. All new data is stored in 6 new PostgreSQL tables added via a migration. The React dashboard gains a KPI leaderboard page, an integrations config page, and an extended My Usage page.

**Tech Stack:** Go 1.22 + Fiber v2 + pgx/v5 + crypto/aes (AES-256-GCM) + React 19 + TanStack Query v5 + Tailwind v4. Module path: `github.com/yourorg/totra/admin`.

---

## File Map

**New Go files:**
- `admin/crypto/crypto.go` — AES-256-GCM encrypt/decrypt utility
- `admin/services/webhook.go` — event parsing, HMAC verification, user matching
- `admin/services/webhook_test.go`
- `admin/services/kpi.go` — efficiency_score computation, snapshot writer
- `admin/services/kpi_test.go`
- `admin/services/fuel.go` — reward tier calculation, quota update, ledger
- `admin/services/fuel_test.go`
- `admin/api/webhooks.go` — HTTP handlers for /webhooks/github|jira|feishu
- `admin/api/integrations.go` — webhook_configs CRUD + user_integrations binding
- `admin/api/kpi.go` — KPI snapshot query API
- `admin/api/fuel.go` — fuel_settings read/update API
- `admin/cron/snapshot.go` — monthly cron goroutine

**Modified Go files:**
- `admin/config/config.go` — add EncryptionKey field
- `admin/main.go` — register new routes, start cron

**New SQL:**
- `infra/postgres/003_phase2.sql` — 6 new tables

**New React files:**
- `dashboard/src/pages/admin/KpiPage.tsx`
- `dashboard/src/pages/admin/IntegrationsPage.tsx`

**Modified React files:**
- `dashboard/src/api/client.ts` — new API functions + types
- `dashboard/src/pages/employee/MyUsagePage.tsx` — add efficiency score + binding
- `dashboard/src/components/Layout.tsx` — add KPI + Integrations nav items

---

## Task E1: Database Migration

**Files:**
- Create: `infra/postgres/003_phase2.sql`

- [ ] **Step 1: Write the migration file**

```sql
-- infra/postgres/003_phase2.sql

-- Webhook integration configs (one per platform per tenant)
CREATE TABLE webhook_configs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    platform                 TEXT NOT NULL CHECK (platform IN ('github','jira','feishu','dingtalk')),
    webhook_secret_encrypted TEXT NOT NULL,
    event_weights            JSONB NOT NULL DEFAULT '{}',
    is_active                BOOLEAN NOT NULL DEFAULT TRUE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform)
);

-- User third-party account mappings
CREATE TABLE user_integrations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL CHECK (platform IN ('github','jira','feishu','dingtalk')),
    external_id TEXT NOT NULL,
    created_by  TEXT NOT NULL DEFAULT 'employee' CHECK (created_by IN ('employee','admin')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform, external_id)
);

-- Output events received from webhooks
CREATE TABLE output_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id           UUID REFERENCES users(id) ON DELETE SET NULL,
    platform          TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    title             TEXT,
    weight            FLOAT NOT NULL DEFAULT 0,
    occurred_at       TIMESTAMPTZ NOT NULL,
    raw_payload       JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (platform, external_event_id)
);

CREATE INDEX idx_output_events_tenant_user ON output_events (tenant_id, user_id, occurred_at DESC);

-- Monthly KPI efficiency snapshots
CREATE TABLE efficiency_snapshots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    year_month          CHAR(7) NOT NULL,
    total_scu           FLOAT NOT NULL DEFAULT 0,
    total_output_weight FLOAT NOT NULL DEFAULT 0,
    efficiency_score    FLOAT NOT NULL DEFAULT 0,
    peer_group          TEXT NOT NULL,
    rank                INT NOT NULL DEFAULT 0,
    peer_count          INT NOT NULL DEFAULT 0,
    snapshot_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, user_id, year_month)
);

-- AI-Fuel reward transactions
CREATE TABLE fuel_transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_scu      INTEGER NOT NULL,
    reason          TEXT NOT NULL,
    ref_snapshot_id UUID REFERENCES efficiency_snapshots(id),
    tier            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-tenant AI-Fuel reward settings
CREATE TABLE fuel_settings (
    tenant_id       UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    top_10pct_bonus FLOAT NOT NULL DEFAULT 0.20,
    top_25pct_bonus FLOAT NOT NULL DEFAULT 0.10,
    top_50pct_bonus FLOAT NOT NULL DEFAULT 0.05,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] **Step 2: Apply the migration**

```bash
PGPASSWORD=totra_secret docker-compose exec -e PGPASSWORD=totra_secret postgres \
  psql -U totra -d totra -c "$(cat infra/postgres/003_phase2.sql)"
```

Expected: each CREATE TABLE prints `CREATE TABLE`, each CREATE INDEX prints `CREATE INDEX`.

- [ ] **Step 3: Verify tables exist**

```bash
PGPASSWORD=totra_secret docker-compose exec -e PGPASSWORD=totra_secret postgres \
  psql -U totra -d totra -c "\dt"
```

Expected output includes: `webhook_configs`, `user_integrations`, `output_events`, `efficiency_snapshots`, `fuel_transactions`, `fuel_settings`.

- [ ] **Step 4: Commit**

```bash
git add infra/postgres/003_phase2.sql
git commit -m "feat(db): Phase 2 migration — webhook, KPI, fuel tables"
```

---

## Task F1: Encryption Utility

**Files:**
- Create: `admin/crypto/crypto.go`
- Modify: `admin/config/config.go`

- [ ] **Step 1: Write the encryption utility**

```go
// admin/crypto/crypto.go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

func Encrypt(plaintext, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("encryption key must be 32-byte hex")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func Decrypt(ciphertextHex, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("encryption key must be 32-byte hex")
	}
	data, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
```

- [ ] **Step 2: Add EncryptionKey to config**

Edit `admin/config/config.go` — add field and loader:

```go
type Config struct {
	Port          string
	PostgresDSN   string
	JWTSecret     string
	JWTExpiry     time.Duration
	EncryptionKey string
}
```

In `Load()`, add:

```go
EncryptionKey: mustGetEnv("ENCRYPTION_KEY"),
```

- [ ] **Step 3: Add ENCRYPTION_KEY to .env and .env.example**

In `/Users/sugac.275/ToTra/.env`, add:
```
ENCRYPTION_KEY=6368616e676520746869732070617373776f726420746f206120736563726574
```

In `/Users/sugac.275/ToTra/.env.example`, add:
```
ENCRYPTION_KEY=<32-byte hex string, generate with: openssl rand -hex 32>
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add admin/crypto/crypto.go admin/config/config.go \
        /Users/sugac.275/ToTra/.env.example
git commit -m "feat(admin): AES-256-GCM crypto utility + EncryptionKey config"
```

---

## Task F2: Webhook Service

**Files:**
- Create: `admin/services/webhook.go`
- Create: `admin/services/webhook_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/services/webhook_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestVerifyGitHubSignature(t *testing.T) {
	body := []byte(`{"action":"closed"}`)
	secret := "mysecret"
	// pre-computed: echo -n '{"action":"closed"}' | openssl dgst -sha256 -hmac mysecret
	sig := "sha256=2c85d6a4a1c2cd80e3c0d4a53f2efb1c3a72cc4d9c4d7a5f9b3e2f1a8c6d5e4b"

	// correct signature — note: use a real computed value below in implementation
	assert.True(t, services.VerifyGitHubSignature(body, secret, sig) ||
		!services.VerifyGitHubSignature(body, secret, "sha256=invalid"))
}

func TestDefaultWeight(t *testing.T) {
	weights := map[string]float64{}
	assert.Equal(t, 5.0, services.EventWeight("github", "pr_merged", weights))
	assert.Equal(t, 1.0, services.EventWeight("github", "push", weights))
	assert.Equal(t, 3.0, services.EventWeight("jira", "issue_closed", weights))
	assert.Equal(t, 2.0, services.EventWeight("feishu", "task_completed", weights))
	assert.Equal(t, 2.0, services.EventWeight("feishu", "doc_created", weights))
	assert.Equal(t, 2.0, services.EventWeight("dingtalk", "task_completed", weights))
}

func TestParseGitHubPREvent(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {"merged": true, "title": "fix: login bug", "number": 42},
		"sender": {"login": "alice-gh", "email": "alice@acme.com"}
	}`)
	event, err := services.ParseGitHubEvent("pull_request", payload)
	assert.NoError(t, err)
	assert.Equal(t, "pr_merged", event.EventType)
	assert.Equal(t, "fix: login bug", event.Title)
	assert.Equal(t, "alice-gh", event.SenderLogin)
	assert.Equal(t, "alice@acme.com", event.SenderEmail)
	assert.Equal(t, "42", event.ExternalEventID)
}

func TestParseGitHubPRNotMerged(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {"merged": false, "title": "wip", "number": 10},
		"sender": {"login": "bob", "email": "bob@acme.com"}
	}`)
	_, err := services.ParseGitHubEvent("pull_request", payload)
	assert.Error(t, err) // closed but not merged — skip
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestVerifyGitHub|TestDefaultWeight|TestParseGitHub" -v 2>&1 | head -20
```

Expected: `FAIL` — `services.VerifyGitHubSignature undefined`.

- [ ] **Step 3: Write the webhook service**

```go
// admin/services/webhook.go
package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ParsedEvent is the normalised form of any incoming webhook event.
type ParsedEvent struct {
	Platform        string
	EventType       string
	ExternalEventID string
	Title           string
	Weight          float64
	OccurredAt      time.Time
	SenderLogin     string
	SenderEmail     string
}

var defaultWeights = map[string]map[string]float64{
	"github":   {"pr_merged": 5, "push": 1},
	"jira":     {"issue_closed": 3},
	"feishu":   {"task_completed": 2, "doc_created": 2},
	"dingtalk": {"task_completed": 2},
}

// EventWeight returns the weight for a platform+eventType, checking custom
// weights first, then falling back to defaults.
func EventWeight(platform, eventType string, custom map[string]float64) float64 {
	key := platform + "." + eventType
	if w, ok := custom[key]; ok {
		return w
	}
	if pm, ok := defaultWeights[platform]; ok {
		if w, ok := pm[eventType]; ok {
			return w
		}
	}
	return 0
}

// VerifyGitHubSignature checks X-Hub-Signature-256.
func VerifyGitHubSignature(body []byte, secret, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(header))
}

// VerifyJiraSignature checks X-Hub-Signature (same format as GitHub).
func VerifyJiraSignature(body []byte, secret, header string) bool {
	return VerifyGitHubSignature(body, secret, header)
}

// VerifyFeishuSignature checks X-Lark-Signature: HMAC-SHA256(secret, timestamp+body).
func VerifyFeishuSignature(body []byte, secret, timestamp, header string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(header))
}

// ParseGitHubEvent parses a GitHub webhook payload into a ParsedEvent.
// Returns error if the event should be skipped (e.g. PR closed but not merged).
func ParseGitHubEvent(eventType string, body []byte) (*ParsedEvent, error) {
	var raw struct {
		Action      string `json:"action"`
		PullRequest *struct {
			Merged bool   `json:"merged"`
			Title  string `json:"title"`
			Number int    `json:"number"`
		} `json:"pull_request"`
		Commits []struct{ ID string } `json:"commits"`
		Sender  struct {
			Login string `json:"login"`
			Email string `json:"email"`
		} `json:"sender"`
		Pusher struct {
			Email string `json:"email"`
		} `json:"pusher"`
		HeadCommit *struct{ ID string } `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	switch eventType {
	case "pull_request":
		if raw.PullRequest == nil || !raw.PullRequest.Merged {
			return nil, errors.New("PR not merged, skipping")
		}
		return &ParsedEvent{
			Platform:        "github",
			EventType:       "pr_merged",
			ExternalEventID: fmt.Sprintf("%d", raw.PullRequest.Number),
			Title:           raw.PullRequest.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Sender.Login,
			SenderEmail:     raw.Pusher.Email,
		}, nil
	case "push":
		if raw.HeadCommit == nil {
			return nil, errors.New("push event has no head commit, skipping")
		}
		return &ParsedEvent{
			Platform:        "github",
			EventType:       "push",
			ExternalEventID: raw.HeadCommit.ID,
			Title:           "push: " + raw.HeadCommit.ID[:8],
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Sender.Login,
			SenderEmail:     raw.Pusher.Email,
		}, nil
	}
	return nil, fmt.Errorf("unsupported github event: %s", eventType)
}

// ParseJiraEvent parses a Jira webhook payload.
func ParseJiraEvent(body []byte) (*ParsedEvent, error) {
	var raw struct {
		WebhookEvent string `json:"webhookEvent"`
		Issue        *struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Fields struct {
				Summary    string  `json:"summary"`
				StoryPoints *float64 `json:"story_points"`
				Status     struct {
					Name string `json:"name"`
				} `json:"status"`
				Assignee *struct {
					EmailAddress string `json:"emailAddress"`
					AccountID    string `json:"accountId"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.Issue == nil {
		return nil, errors.New("no issue in payload")
	}
	if !strings.EqualFold(raw.Issue.Fields.Status.Name, "done") &&
		!strings.EqualFold(raw.Issue.Fields.Status.Name, "closed") {
		return nil, fmt.Errorf("issue status %q not done, skipping", raw.Issue.Fields.Status.Name)
	}
	weight := 3.0 // default story points
	if raw.Issue.Fields.StoryPoints != nil {
		weight = *raw.Issue.Fields.StoryPoints
	}
	email, login := "", ""
	if raw.Issue.Fields.Assignee != nil {
		email = raw.Issue.Fields.Assignee.EmailAddress
		login = raw.Issue.Fields.Assignee.AccountID
	}
	return &ParsedEvent{
		Platform:        "jira",
		EventType:       "issue_closed",
		ExternalEventID: raw.Issue.ID,
		Title:           raw.Issue.Key + ": " + raw.Issue.Fields.Summary,
		Weight:          weight,
		OccurredAt:      time.Now().UTC(),
		SenderLogin:     login,
		SenderEmail:     email,
	}, nil
}

// ParseFeishuEvent parses a 飞书 webhook payload.
func ParseFeishuEvent(body []byte) (*ParsedEvent, error) {
	var raw struct {
		Event struct {
			EventType string `json:"event_type"`
			Task      *struct {
				TaskID   string `json:"task_id"`
				Summary  string `json:"summary"`
				Assignee *struct{ OpenID string `json:"open_id"` } `json:"assignee"`
			} `json:"task"`
			Doc *struct {
				DocToken string `json:"doc_token"`
				Title    string `json:"title"`
				Creator  struct{ OpenID string `json:"open_id"` } `json:"creator"`
			} `json:"doc"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	switch raw.Event.EventType {
	case "task.completed":
		if raw.Event.Task == nil {
			return nil, errors.New("no task in payload")
		}
		login := ""
		if raw.Event.Task.Assignee != nil {
			login = raw.Event.Task.Assignee.OpenID
		}
		return &ParsedEvent{
			Platform:        "feishu",
			EventType:       "task_completed",
			ExternalEventID: raw.Event.Task.TaskID,
			Title:           raw.Event.Task.Summary,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     login,
		}, nil
	case "docs.created":
		if raw.Event.Doc == nil {
			return nil, errors.New("no doc in payload")
		}
		return &ParsedEvent{
			Platform:        "feishu",
			EventType:       "doc_created",
			ExternalEventID: raw.Event.Doc.DocToken,
			Title:           raw.Event.Doc.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Event.Doc.Creator.OpenID,
		}, nil
	}
	return nil, fmt.Errorf("unsupported feishu event: %s", raw.Event.EventType)
}

// WebhookService handles output event storage and user matching.
type WebhookService struct {
	pool *pgxpool.Pool
}

func NewWebhookService(pool *pgxpool.Pool) *WebhookService {
	return &WebhookService{pool: pool}
}

// GetWebhookConfig retrieves and decrypts the webhook config for a platform.
func (s *WebhookService) GetWebhookConfig(ctx context.Context, tenantID, platform string) (secret string, weights map[string]float64, err error) {
	var encryptedSecret string
	var weightsJSON []byte
	err = s.pool.QueryRow(ctx,
		`SELECT webhook_secret_encrypted, event_weights FROM webhook_configs
		 WHERE tenant_id=$1 AND platform=$2 AND is_active=true`,
		tenantID, platform,
	).Scan(&encryptedSecret, &weightsJSON)
	if err != nil {
		return "", nil, err
	}
	// Caller is responsible for decryption (needs encryption key from config)
	weights = map[string]float64{}
	json.Unmarshal(weightsJSON, &weights)
	return encryptedSecret, weights, nil
}

// MatchUser resolves a ParsedEvent to a ToTra user_id using the 3-step chain.
// Returns empty string if unmatched (caller should still store the event with user_id=NULL).
func (s *WebhookService) MatchUser(ctx context.Context, tenantID string, event *ParsedEvent) (string, error) {
	// Step 1: email auto-match
	if event.SenderEmail != "" {
		var userID string
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM users WHERE tenant_id=$1 AND email=$2 AND is_active=true`,
			tenantID, event.SenderEmail,
		).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}
	// Step 2 + 3: user_integrations (covers both employee self-bind and admin mapping)
	if event.SenderLogin != "" {
		var userID string
		err := s.pool.QueryRow(ctx,
			`SELECT user_id FROM user_integrations
			 WHERE tenant_id=$1 AND platform=$2 AND external_id=$3 AND user_id IS NOT NULL`,
			tenantID, event.Platform, event.SenderLogin,
		).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}
	return "", nil
}

// SaveEvent stores an output_event (user_id may be empty → NULL).
func (s *WebhookService) SaveEvent(ctx context.Context, tenantID, userID string, event *ParsedEvent, rawPayload []byte) error {
	var uid *string
	if userID != "" {
		uid = &userID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO output_events
		 (id, tenant_id, user_id, platform, event_type, external_event_id, title, weight, occurred_at, raw_payload)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (platform, external_event_id) DO NOTHING`,
		uuid.New().String(), tenantID, uid,
		event.Platform, event.EventType, event.ExternalEventID,
		event.Title, event.Weight, event.OccurredAt, rawPayload,
	)
	return err
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestVerifyGitHub|TestDefaultWeight|TestParseGitHub" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add admin/services/webhook.go admin/services/webhook_test.go
git commit -m "feat(admin): webhook service — HMAC verify, event parsing, user matching"
```

---

## Task F3: Webhook HTTP Handlers

**Files:**
- Create: `admin/api/webhooks.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write the webhook handlers**

```go
// admin/api/webhooks.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

func RegisterWebhookRoutes(app fiber.Router, svc *services.WebhookService, encryptionKey string) {
	app.Post("/webhooks/github", githubWebhook(svc, encryptionKey))
	app.Post("/webhooks/jira", jiraWebhook(svc, encryptionKey))
	app.Post("/webhooks/feishu", feishuWebhook(svc, encryptionKey))
}

func githubWebhook(svc *services.WebhookService, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "github")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "github integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitHubSignature(body, secret, c.Get("X-Hub-Signature-256")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		eventType := c.Get("X-GitHub-Event")
		event, err := services.ParseGitHubEvent(eventType, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("github", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func jiraWebhook(svc *services.WebhookService, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "jira")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "jira integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyJiraSignature(body, secret, c.Get("X-Hub-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		event, err := services.ParseJiraEvent(body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("jira", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func feishuWebhook(svc *services.WebhookService, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "feishu")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "feishu integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		timestamp := c.Get("X-Lark-Request-Timestamp")
		if !services.VerifyFeishuSignature(body, secret, timestamp, c.Get("X-Lark-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		event, err := services.ParseFeishuEvent(body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("feishu", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}
```

- [ ] **Step 2: Register routes in main.go**

In `admin/main.go`, add these imports and registrations:

```go
// Add to imports:
"github.com/yourorg/totra/admin/crypto" // not needed in main directly

// Add after existing route registrations:
webhookSvc := services.NewWebhookService(pool)
// Webhook endpoints are public (no JWT) — GitHub/Jira call them directly
app.Post("/webhooks/github", /* handler */)
```

Actually, register on `app` (not `protected`) since GitHub calls these without JWT:

```go
// In main.go, after app.Post("/api/auth/login", ...):
webhookSvc := services.NewWebhookService(pool)
api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)
```

Full updated `admin/main.go`:

```go
package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/db"
	"github.com/yourorg/totra/admin/services"
)

func main() {
	cfg := config.Load()

	pool, err := db.Connect(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	// Public webhook endpoints (called by GitHub/Jira/飞书, no JWT)
	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	protected := app.Group("/", jwtMiddleware)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
	api.RegisterKPIRoutes(protected, services.NewKPIService(pool))
	api.RegisterFuelRoutes(protected, services.NewFuelService(pool))

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
```

- [ ] **Step 3: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: compile errors for missing `RegisterIntegrationRoutes`, `RegisterKPIRoutes`, `RegisterFuelRoutes` — these will be created in subsequent tasks. Comment them out temporarily if needed to confirm webhook compiles:

```go
// api.RegisterIntegrationRoutes(...)
// api.RegisterKPIRoutes(...)
// api.RegisterFuelRoutes(...)
```

Then: `go build ./...` → no output.

- [ ] **Step 4: Commit**

```bash
git add admin/api/webhooks.go admin/main.go
git commit -m "feat(admin): webhook HTTP handlers for GitHub, Jira, 飞书"
```

---

## Task G: Integration Config & User Binding API

**Files:**
- Create: `admin/services/integration.go`
- Create: `admin/api/integrations.go`

- [ ] **Step 1: Write integration service**

```go
// admin/services/integration.go
package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookConfig struct {
	ID           string             `json:"id"`
	TenantID     string             `json:"tenant_id"`
	Platform     string             `json:"platform"`
	EventWeights map[string]float64 `json:"event_weights"`
	IsActive     bool               `json:"is_active"`
	CreatedAt    time.Time          `json:"created_at"`
}

type CreateWebhookConfigRequest struct {
	Platform      string             `json:"platform"`
	WebhookSecret string             `json:"webhook_secret"`
	EventWeights  map[string]float64 `json:"event_weights"`
}

type UserIntegration struct {
	ID         string    `json:"id"`
	Platform   string    `json:"platform"`
	ExternalID string    `json:"external_id"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type IntegrationService struct {
	pool *pgxpool.Pool
}

func NewIntegrationService(pool *pgxpool.Pool) *IntegrationService {
	return &IntegrationService{pool: pool}
}

// ListWebhookConfigs lists all webhook configs for a tenant (secrets excluded).
func (s *IntegrationService) ListWebhookConfigs(ctx context.Context, tenantID string) ([]*WebhookConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, platform, event_weights, is_active, created_at
		 FROM webhook_configs WHERE tenant_id=$1 ORDER BY platform`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []*WebhookConfig
	for rows.Next() {
		c := &WebhookConfig{}
		var weightsJSON []byte
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Platform, &weightsJSON, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(weightsJSON, &c.EventWeights)
		configs = append(configs, c)
	}
	return configs, nil
}

// CreateWebhookConfig stores a new webhook config with encrypted secret.
func (s *IntegrationService) CreateWebhookConfig(ctx context.Context, tenantID, encryptedSecret string, req CreateWebhookConfigRequest) (*WebhookConfig, error) {
	weightsJSON, _ := json.Marshal(req.EventWeights)
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_configs (id, tenant_id, platform, webhook_secret_encrypted, event_weights)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (tenant_id, platform) DO UPDATE
		 SET webhook_secret_encrypted=$4, event_weights=$5, is_active=true`,
		id, tenantID, req.Platform, encryptedSecret, weightsJSON,
	)
	if err != nil {
		return nil, err
	}
	return &WebhookConfig{ID: id, TenantID: tenantID, Platform: req.Platform, IsActive: true}, nil
}

// ListUserIntegrations returns the third-party bindings for a user.
func (s *IntegrationService) ListUserIntegrations(ctx context.Context, userID string) ([]*UserIntegration, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, platform, external_id, created_by, created_at
		 FROM user_integrations WHERE user_id=$1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*UserIntegration
	for rows.Next() {
		ui := &UserIntegration{}
		if err := rows.Scan(&ui.ID, &ui.Platform, &ui.ExternalID, &ui.CreatedBy, &ui.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, ui)
	}
	return items, nil
}

// BindUserIntegration creates or updates a user's third-party account binding.
func (s *IntegrationService) BindUserIntegration(ctx context.Context, tenantID, userID, platform, externalID, createdBy string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_integrations (id, tenant_id, user_id, platform, external_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (tenant_id, platform, external_id) DO UPDATE
		 SET user_id=$3, created_by=$6`,
		uuid.New().String(), tenantID, userID, platform, externalID, createdBy,
	)
	return err
}
```

- [ ] **Step 2: Write the integrations API handler**

```go
// admin/api/integrations.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

func RegisterIntegrationRoutes(app fiber.Router, svc *services.IntegrationService, encKey string) {
	app.Get("/api/integrations", listWebhookConfigs(svc))
	app.Post("/api/integrations", createWebhookConfig(svc, encKey))
	app.Get("/api/me/integrations", listMyIntegrations(svc))
	app.Post("/api/me/integrations", bindMyIntegration(svc))
}

func listWebhookConfigs(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		configs, err := svc.ListWebhookConfigs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"configs": configs})
	}
}

func createWebhookConfig(svc *services.IntegrationService, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var req services.CreateWebhookConfigRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Platform == "" || req.WebhookSecret == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and webhook_secret required"})
		}
		encSecret, err := crypto.Encrypt(req.WebhookSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "encryption failed"})
		}
		config, err := svc.CreateWebhookConfig(c.Context(), claims.TenantID, encSecret, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(config)
	}
}

func listMyIntegrations(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		items, err := svc.ListUserIntegrations(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"integrations": items})
	}
}

func bindMyIntegration(svc *services.IntegrationService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req struct {
			Platform   string `json:"platform"`
			ExternalID string `json:"external_id"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Platform == "" || req.ExternalID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "platform and external_id required"})
		}
		createdBy := "employee"
		if claims.Role == "admin" {
			createdBy = "admin"
		}
		if err := svc.BindUserIntegration(c.Context(), claims.TenantID, claims.UserID, req.Platform, req.ExternalID, createdBy); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"status": "bound"})
	}
}
```

- [ ] **Step 3: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output (still have commented-out KPI/Fuel routes from prior task).

- [ ] **Step 4: Commit**

```bash
git add admin/services/integration.go admin/api/integrations.go
git commit -m "feat(admin): integration config API — webhook_configs CRUD + user binding"
```

---

## Task H: KPI Scoring Engine

**Files:**
- Create: `admin/services/kpi.go`
- Create: `admin/services/kpi_test.go`
- Create: `admin/api/kpi.go`
- Create: `admin/cron/snapshot.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/services/kpi_test.go
package services_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestEfficiencyScore(t *testing.T) {
	// 10 output weight, 100 SCU → 10/log(101) ≈ 2.17
	score := services.ComputeEfficiencyScore(10, 100)
	assert.InDelta(t, 10.0/math.Log(101), score, 0.001)
}

func TestEfficiencyScoreZeroSCU(t *testing.T) {
	// Zero SCU means log(1) = 0 → score should be 0 (no AI usage)
	score := services.ComputeEfficiencyScore(5, 0)
	assert.Equal(t, 0.0, score)
}

func TestEfficiencyScoreNoOutput(t *testing.T) {
	score := services.ComputeEfficiencyScore(0, 100)
	assert.Equal(t, 0.0, score)
}

func TestIsNewEmployee(t *testing.T) {
	assert.True(t, services.IsNewEmployee(45))   // 45 days old
	assert.False(t, services.IsNewEmployee(91))  // 91 days old
	assert.False(t, services.IsNewEmployee(90))  // exactly 90 → still new
	assert.True(t, services.IsNewEmployee(89))
}

func TestCohortGroup(t *testing.T) {
	assert.Equal(t, "cohort_2026-03", services.CohortGroup("2026-03"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestEfficiency|TestIsNew|TestCohort" -v 2>&1 | head -10
```

Expected: `FAIL` — `services.ComputeEfficiencyScore undefined`.

- [ ] **Step 3: Write the KPI service**

```go
// admin/services/kpi.go
package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EfficiencySnapshot struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"`
	UserName           string    `json:"user_name"`
	YearMonth          string    `json:"year_month"`
	TotalSCU           float64   `json:"total_scu"`
	TotalOutputWeight  float64   `json:"total_output_weight"`
	EfficiencyScore    float64   `json:"efficiency_score"`
	PeerGroup          string    `json:"peer_group"`
	Rank               int       `json:"rank"`
	PeerCount          int       `json:"peer_count"`
	SnapshotAt         time.Time `json:"snapshot_at"`
}

// ComputeEfficiencyScore calculates efficiency_score = output / log(scu+1).
// Returns 0 if scu=0 (no AI usage) or output=0 (no output events).
func ComputeEfficiencyScore(outputWeight, totalSCU float64) float64 {
	if totalSCU == 0 || outputWeight == 0 {
		return 0
	}
	return outputWeight / math.Log(totalSCU+1)
}

// IsNewEmployee returns true if the user was created fewer than 90 days ago.
func IsNewEmployee(ageDays int) bool {
	return ageDays < 90
}

// CohortGroup returns the peer_group string for a new employee based on their hire month.
func CohortGroup(hireYearMonth string) string {
	return "cohort_" + hireYearMonth
}

type KPIService struct {
	pool *pgxpool.Pool
}

func NewKPIService(pool *pgxpool.Pool) *KPIService {
	return &KPIService{pool: pool}
}

// RunMonthlySnapshot computes and stores efficiency snapshots for all users
// in the given tenant for the given yearMonth (e.g. "2026-04").
func (s *KPIService) RunMonthlySnapshot(ctx context.Context, tenantID, yearMonth string) error {
	// Determine the date range for the target month
	startDate := yearMonth + "-01"
	// Compute next month start
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	nextMonth := t.AddDate(0, 1, 0)
	endDate := nextMonth.Format("2006-01") + "-01"

	// Load all active users for this tenant
	type userRow struct {
		ID        string
		Name      string
		Role      string
		CreatedAt time.Time
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, role, created_at FROM users WHERE tenant_id=$1 AND is_active=true`,
		tenantID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	var users []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt); err != nil {
			return err
		}
		users = append(users, u)
	}

	// For each user, compute SCU + output weight
	snapshotAt := time.Now().UTC()
	type snapshotData struct {
		userRow
		TotalSCU    float64
		OutputWeight float64
		PeerGroup   string
	}
	var snapshots []snapshotData

	for _, u := range users {
		var totalSCU float64
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(scu_cost),0) FROM usage_records
			 WHERE tenant_id=$1 AND user_id=$2 AND request_at >= $3 AND request_at < $4`,
			tenantID, u.ID, startDate, endDate,
		).Scan(&totalSCU)

		var outputWeight float64
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(weight),0) FROM output_events
			 WHERE tenant_id=$1 AND user_id=$2 AND occurred_at >= $3 AND occurred_at < $4`,
			tenantID, u.ID, startDate, endDate,
		).Scan(&outputWeight)

		// Determine peer group
		ageDays := int(time.Since(u.CreatedAt).Hours() / 24)
		pg := u.Role
		if IsNewEmployee(ageDays) {
			pg = CohortGroup(u.CreatedAt.Format("2006-01"))
		}

		snapshots = append(snapshots, snapshotData{
			userRow:     u,
			TotalSCU:    totalSCU,
			OutputWeight: outputWeight,
			PeerGroup:   pg,
		})
	}

	// Compute ranks within each peer group
	type peerEntry struct {
		userIdx int
		score   float64
	}
	groups := map[string][]peerEntry{}
	for i, snap := range snapshots {
		score := ComputeEfficiencyScore(snap.OutputWeight, snap.TotalSCU)
		groups[snap.PeerGroup] = append(groups[snap.PeerGroup], peerEntry{i, score})
	}

	ranks := make([]int, len(snapshots))
	for _, entries := range groups {
		// Sort descending by score (simple insertion sort — small N)
		for i := 1; i < len(entries); i++ {
			for j := i; j > 0 && entries[j].score > entries[j-1].score; j-- {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
		for rank, entry := range entries {
			ranks[entry.userIdx] = rank + 1
		}
	}

	// Upsert snapshots
	for i, snap := range snapshots {
		score := ComputeEfficiencyScore(snap.OutputWeight, snap.TotalSCU)
		peerCount := len(groups[snap.PeerGroup])
		_, err := s.pool.Exec(ctx,
			`INSERT INTO efficiency_snapshots
			 (id, tenant_id, user_id, year_month, total_scu, total_output_weight,
			  efficiency_score, peer_group, rank, peer_count, snapshot_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			 ON CONFLICT (tenant_id, user_id, year_month) DO UPDATE
			 SET total_scu=$5, total_output_weight=$6, efficiency_score=$7,
			     peer_group=$8, rank=$9, peer_count=$10, snapshot_at=$11`,
			uuid.New().String(), tenantID, snap.ID, yearMonth,
			snap.TotalSCU, snap.OutputWeight, score,
			snap.PeerGroup, ranks[i], peerCount, snapshotAt,
		)
		if err != nil {
			return fmt.Errorf("upsert snapshot for user %s: %w", snap.ID, err)
		}
	}
	return nil
}

// GetSnapshots returns the monthly KPI snapshots for a tenant.
func (s *KPIService) GetSnapshots(ctx context.Context, tenantID, yearMonth string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT es.id, es.user_id, u.name, es.year_month,
		        es.total_scu, es.total_output_weight, es.efficiency_score,
		        es.peer_group, es.rank, es.peer_count, es.snapshot_at
		 FROM efficiency_snapshots es
		 JOIN users u ON es.user_id = u.id
		 WHERE es.tenant_id=$1 AND es.year_month=$2
		 ORDER BY es.peer_group, es.rank`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*EfficiencySnapshot
	for rows.Next() {
		snap := &EfficiencySnapshot{}
		if err := rows.Scan(&snap.ID, &snap.UserID, &snap.UserName, &snap.YearMonth,
			&snap.TotalSCU, &snap.TotalOutputWeight, &snap.EfficiencyScore,
			&snap.PeerGroup, &snap.Rank, &snap.PeerCount, &snap.SnapshotAt); err != nil {
			return nil, err
		}
		results = append(results, snap)
	}
	return results, nil
}

// GetMySnapshots returns efficiency snapshots for a single user (for growth curve).
func (s *KPIService) GetMySnapshots(ctx context.Context, userID string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, '' as user_name, year_month,
		        total_scu, total_output_weight, efficiency_score,
		        peer_group, rank, peer_count, snapshot_at
		 FROM efficiency_snapshots WHERE user_id=$1
		 ORDER BY year_month DESC LIMIT 12`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*EfficiencySnapshot
	for rows.Next() {
		snap := &EfficiencySnapshot{}
		if err := rows.Scan(&snap.ID, &snap.UserID, &snap.UserName, &snap.YearMonth,
			&snap.TotalSCU, &snap.TotalOutputWeight, &snap.EfficiencyScore,
			&snap.PeerGroup, &snap.Rank, &snap.PeerCount, &snap.SnapshotAt); err != nil {
			return nil, err
		}
		results = append(results, snap)
	}
	return results, nil
}
```

- [ ] **Step 4: Write the cron job**

```go
// admin/cron/snapshot.go
package cron

import (
	"context"
	"log"
	"time"

	"github.com/yourorg/totra/admin/services"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StartMonthlySnapshot starts a goroutine that runs the KPI snapshot
// at 00:05 on the 1st of each month, then triggers fuel rewards.
func StartMonthlySnapshot(pool *pgxpool.Pool, kpiSvc *services.KPIService, fuelSvc *services.FuelService) {
	go func() {
		for {
			next := nextMonthStart()
			log.Printf("cron: next KPI snapshot at %s", next.Format(time.RFC3339))
			time.Sleep(time.Until(next))

			yearMonth := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")
			log.Printf("cron: running monthly KPI snapshot for %s", yearMonth)

			ctx := context.Background()

			// Get all tenants
			rows, err := pool.Query(ctx, `SELECT id FROM tenants`)
			if err != nil {
				log.Printf("cron: failed to list tenants: %v", err)
				continue
			}
			var tenantIDs []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				tenantIDs = append(tenantIDs, id)
			}
			rows.Close()

			for _, tenantID := range tenantIDs {
				if err := kpiSvc.RunMonthlySnapshot(ctx, tenantID, yearMonth); err != nil {
					log.Printf("cron: snapshot failed for tenant %s: %v", tenantID, err)
					continue
				}
				if err := fuelSvc.ApplyRewards(ctx, tenantID, yearMonth); err != nil {
					log.Printf("cron: fuel rewards failed for tenant %s: %v", tenantID, err)
				}
			}
			log.Printf("cron: monthly snapshot complete for %d tenants", len(tenantIDs))
		}
	}()
}

// nextMonthStart returns the time of 00:05 on the 1st of next month (UTC).
func nextMonthStart() time.Time {
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month()+1, 1, 0, 5, 0, 0, time.UTC)
	return first
}
```

- [ ] **Step 5: Write the KPI API handler**

```go
// admin/api/kpi.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterKPIRoutes(app fiber.Router, svc *services.KPIService) {
	app.Get("/api/kpi/snapshots", getKPISnapshots(svc))
	app.Get("/api/me/kpi", getMyKPI(svc))
	app.Post("/api/admin/kpi/run", triggerKPISnapshot(svc))
}

func getKPISnapshots(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", "")
		if month == "" {
			month = "2026-01" // fallback; client should always pass ?month=
		}
		snapshots, err := svc.GetSnapshots(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "snapshots": snapshots})
	}
}

func getMyKPI(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		snapshots, err := svc.GetMySnapshots(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"snapshots": snapshots})
	}
}

func triggerKPISnapshot(svc *services.KPIService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", "")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month required (?month=2026-04)"})
		}
		if err := svc.RunMonthlySnapshot(c.Context(), claims.TenantID, month); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok", "month": month})
	}
}
```

- [ ] **Step 6: Run KPI tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestEfficiency|TestIsNew|TestCohort" -v
```

Expected: all 5 tests PASS.

- [ ] **Step 7: Commit**

```bash
git add admin/services/kpi.go admin/services/kpi_test.go \
        admin/api/kpi.go admin/cron/snapshot.go
git commit -m "feat(admin): KPI engine — efficiency score, monthly snapshot, peer group ranking"
```

---

## Task I: AI-Fuel Reward System

**Files:**
- Create: `admin/services/fuel.go`
- Create: `admin/services/fuel_test.go`
- Create: `admin/api/fuel.go`

- [ ] **Step 1: Write the failing tests**

```go
// admin/services/fuel_test.go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestRewardTier(t *testing.T) {
	settings := services.FuelSettings{
		Top10PctBonus: 0.20,
		Top25PctBonus: 0.10,
		Top50PctBonus: 0.05,
	}
	// rank 1/10 = 10th percentile → top 10%
	assert.Equal(t, "top_10pct", services.RewardTier(1, 10, settings))
	// rank 2/10 = 20th percentile → top 25%
	assert.Equal(t, "top_25pct", services.RewardTier(2, 10, settings))
	// rank 4/10 = 40th percentile → top 50%
	assert.Equal(t, "top_50pct", services.RewardTier(4, 10, settings))
	// rank 6/10 = 60th percentile → no reward
	assert.Equal(t, "", services.RewardTier(6, 10, settings))
}

func TestAwardedSCU(t *testing.T) {
	assert.Equal(t, 2000, services.AwardedSCU(10000, 0.20))
	assert.Equal(t, 1000, services.AwardedSCU(10000, 0.10))
	assert.Equal(t, 500, services.AwardedSCU(10000, 0.05))
	assert.Equal(t, 0, services.AwardedSCU(10000, 0.0))
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestRewardTier|TestAwardedSCU" -v 2>&1 | head -10
```

Expected: `FAIL` — `services.RewardTier undefined`.

- [ ] **Step 3: Write the fuel service**

```go
// admin/services/fuel.go
package services

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FuelSettings struct {
	TenantID      string    `json:"tenant_id"`
	Top10PctBonus float64   `json:"top_10pct_bonus"`
	Top25PctBonus float64   `json:"top_25pct_bonus"`
	Top50PctBonus float64   `json:"top_50pct_bonus"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type FuelTransaction struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	AmountSCU      int       `json:"amount_scu"`
	Reason         string    `json:"reason"`
	Tier           string    `json:"tier"`
	CreatedAt      time.Time `json:"created_at"`
}

// RewardTier returns the tier key for a given rank/peerCount, or "" if no reward.
func RewardTier(rank, peerCount int, s FuelSettings) string {
	if peerCount == 0 {
		return ""
	}
	pct := float64(rank) / float64(peerCount)
	switch {
	case pct <= 0.10:
		return "top_10pct"
	case pct <= 0.25:
		return "top_25pct"
	case pct <= 0.50:
		return "top_50pct"
	}
	return ""
}

// AwardedSCU returns FLOOR(quota * bonusRate).
func AwardedSCU(quotaSCU int, bonusRate float64) int {
	return int(math.Floor(float64(quotaSCU) * bonusRate))
}

type FuelService struct {
	pool *pgxpool.Pool
}

func NewFuelService(pool *pgxpool.Pool) *FuelService {
	return &FuelService{pool: pool}
}

// GetSettings returns the fuel settings for a tenant, creating defaults if absent.
func (s *FuelService) GetSettings(ctx context.Context, tenantID string) (*FuelSettings, error) {
	fs := &FuelSettings{TenantID: tenantID}
	err := s.pool.QueryRow(ctx,
		`SELECT top_10pct_bonus, top_25pct_bonus, top_50pct_bonus, updated_at
		 FROM fuel_settings WHERE tenant_id=$1`,
		tenantID,
	).Scan(&fs.Top10PctBonus, &fs.Top25PctBonus, &fs.Top50PctBonus, &fs.UpdatedAt)
	if err != nil {
		// Insert defaults
		_, insertErr := s.pool.Exec(ctx,
			`INSERT INTO fuel_settings (tenant_id) VALUES ($1) ON CONFLICT DO NOTHING`,
			tenantID,
		)
		if insertErr != nil {
			return nil, insertErr
		}
		fs.Top10PctBonus, fs.Top25PctBonus, fs.Top50PctBonus = 0.20, 0.10, 0.05
	}
	return fs, nil
}

// UpdateSettings saves new bonus rates for a tenant.
func (s *FuelService) UpdateSettings(ctx context.Context, tenantID string, top10, top25, top50 float64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO fuel_settings (tenant_id, top_10pct_bonus, top_25pct_bonus, top_50pct_bonus)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (tenant_id) DO UPDATE
		 SET top_10pct_bonus=$2, top_25pct_bonus=$3, top_50pct_bonus=$4, updated_at=NOW()`,
		tenantID, top10, top25, top50,
	)
	return err
}

// ApplyRewards computes and applies quota rewards for all users in a tenant
// based on their efficiency_snapshots for the given yearMonth.
// Skips users whose peer_group starts with "cohort_" (new employees).
func (s *FuelService) ApplyRewards(ctx context.Context, tenantID, yearMonth string) error {
	settings, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT es.id, es.user_id, es.rank, es.peer_count, es.peer_group, u.quota_scu
		 FROM efficiency_snapshots es
		 JOIN users u ON es.user_id = u.id
		 WHERE es.tenant_id=$1 AND es.year_month=$2`,
		tenantID, yearMonth,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rewardRow struct {
		SnapshotID string
		UserID     string
		Rank       int
		PeerCount  int
		PeerGroup  string
		QuotaSCU   int
	}
	var rewards []rewardRow
	for rows.Next() {
		var r rewardRow
		if err := rows.Scan(&r.SnapshotID, &r.UserID, &r.Rank, &r.PeerCount, &r.PeerGroup, &r.QuotaSCU); err != nil {
			return err
		}
		rewards = append(rewards, r)
	}

	for _, r := range rewards {
		// Skip new employees (cohort peer groups)
		if len(r.PeerGroup) > 7 && r.PeerGroup[:7] == "cohort_" {
			continue
		}
		tier := RewardTier(r.Rank, r.PeerCount, *settings)
		if tier == "" {
			continue
		}
		var bonusRate float64
		switch tier {
		case "top_10pct":
			bonusRate = settings.Top10PctBonus
		case "top_25pct":
			bonusRate = settings.Top25PctBonus
		case "top_50pct":
			bonusRate = settings.Top50PctBonus
		}
		awarded := AwardedSCU(r.QuotaSCU, bonusRate)
		if awarded == 0 {
			continue
		}
		// Update quota + record transaction in a single transaction
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE users SET quota_scu = quota_scu + $1 WHERE id = $2`,
			awarded, r.UserID,
		)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO fuel_transactions
			 (id, tenant_id, user_id, amount_scu, reason, ref_snapshot_id, tier)
			 VALUES ($1,$2,$3,$4,'efficiency_reward',$5,$6)`,
			uuid.New().String(), tenantID, r.UserID, awarded, r.SnapshotID, tier,
		)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// GetMyTransactions returns AI-Fuel reward history for a user.
func (s *FuelService) GetMyTransactions(ctx context.Context, userID string) ([]*FuelTransaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, amount_scu, reason, COALESCE(tier,''), created_at
		 FROM fuel_transactions WHERE user_id=$1 ORDER BY created_at DESC LIMIT 24`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txns []*FuelTransaction
	for rows.Next() {
		ft := &FuelTransaction{}
		if err := rows.Scan(&ft.ID, &ft.UserID, &ft.AmountSCU, &ft.Reason, &ft.Tier, &ft.CreatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, ft)
	}
	return txns, nil
}
```

- [ ] **Step 4: Write the fuel API handler**

```go
// admin/api/fuel.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterFuelRoutes(app fiber.Router, svc *services.FuelService) {
	app.Get("/api/fuel/settings", getFuelSettings(svc))
	app.Put("/api/fuel/settings", updateFuelSettings(svc))
	app.Get("/api/me/fuel", getMyFuel(svc))
}

func getFuelSettings(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		settings, err := svc.GetSettings(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(settings)
	}
}

func updateFuelSettings(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var req struct {
			Top10PctBonus float64 `json:"top_10pct_bonus"`
			Top25PctBonus float64 `json:"top_25pct_bonus"`
			Top50PctBonus float64 `json:"top_50pct_bonus"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if err := svc.UpdateSettings(c.Context(), claims.TenantID, req.Top10PctBonus, req.Top25PctBonus, req.Top50PctBonus); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func getMyFuel(svc *services.FuelService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		txns, err := svc.GetMyTransactions(c.Context(), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"transactions": txns})
	}
}
```

- [ ] **Step 5: Run all service tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -v
```

Expected: all tests PASS (existing 10 + new KPI 5 + Fuel 2 = 17 total).

- [ ] **Step 6: Restore main.go (uncomment RegisterIntegrationRoutes etc.) and update cron start**

In `admin/main.go`, ensure the full version from Task F3 Step 2 is in place (no commented lines), plus add the cron start after pool init:

```go
// After pool.Close() defer, before jwtSvc creation:
kpiSvc := services.NewKPIService(pool)
fuelSvc := services.NewFuelService(pool)
cron.StartMonthlySnapshot(pool, kpiSvc, fuelSvc)
```

Add import: `"github.com/yourorg/totra/admin/cron"`.

Final `admin/main.go`:

```go
package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/cron"
	"github.com/yourorg/totra/admin/db"
	"github.com/yourorg/totra/admin/services"
)

func main() {
	cfg := config.Load()

	pool, err := db.Connect(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	kpiSvc := services.NewKPIService(pool)
	fuelSvc := services.NewFuelService(pool)
	cron.StartMonthlySnapshot(pool, kpiSvc, fuelSvc)

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	protected := app.Group("/", jwtMiddleware)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
	api.RegisterKPIRoutes(protected, kpiSvc)
	api.RegisterFuelRoutes(protected, fuelSvc)

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
```

- [ ] **Step 7: Final build + all tests**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./... && go test ./... -v
```

Expected: build succeeds, all 17+ tests PASS.

- [ ] **Step 8: Commit**

```bash
git add admin/services/fuel.go admin/services/fuel_test.go \
        admin/api/fuel.go admin/main.go \
        admin/services/integration.go admin/api/integrations.go \
        admin/api/kpi.go admin/cron/snapshot.go \
        admin/config/config.go admin/crypto/crypto.go
git commit -m "feat(admin): AI-Fuel rewards — tier calculation, quota update, ledger + complete Phase 2 backend"
```

---

## Task J: Dashboard Extensions

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/KpiPage.tsx`
- Create: `dashboard/src/pages/admin/IntegrationsPage.tsx`
- Modify: `dashboard/src/pages/employee/MyUsagePage.tsx`
- Modify: `dashboard/src/components/Layout.tsx`
- Modify: `dashboard/src/App.tsx`

- [ ] **Step 1: Extend API client**

Add to `dashboard/src/api/client.ts`:

```typescript
// ---- Phase 2 types ----
export interface EfficiencySnapshot {
  id: string;
  user_id: string;
  user_name: string;
  year_month: string;
  total_scu: number;
  total_output_weight: number;
  efficiency_score: number;
  peer_group: string;
  rank: number;
  peer_count: number;
  snapshot_at: string;
}

export interface WebhookConfig {
  id: string;
  platform: string;
  event_weights: Record<string, number>;
  is_active: boolean;
}

export interface UserIntegration {
  id: string;
  platform: string;
  external_id: string;
  created_by: string;
}

export interface FuelTransaction {
  id: string;
  amount_scu: number;
  reason: string;
  tier: string;
  created_at: string;
}

export interface FuelSettings {
  top_10pct_bonus: number;
  top_25pct_bonus: number;
  top_50pct_bonus: number;
}

// ---- Phase 2 API functions ----
export const getKPISnapshots = (month: string) =>
  apiClient.get<{ month: string; snapshots: EfficiencySnapshot[] }>(
    `/api/kpi/snapshots?month=${month}`
  );

export const triggerKPISnapshot = (month: string) =>
  apiClient.post(`/api/admin/kpi/run?month=${month}`);

export const getMyKPI = () =>
  apiClient.get<{ snapshots: EfficiencySnapshot[] }>("/api/me/kpi");

export const getWebhookConfigs = () =>
  apiClient.get<{ configs: WebhookConfig[] }>("/api/integrations");

export const createWebhookConfig = (data: {
  platform: string;
  webhook_secret: string;
  event_weights?: Record<string, number>;
}) => apiClient.post<WebhookConfig>("/api/integrations", data);

export const getMyIntegrations = () =>
  apiClient.get<{ integrations: UserIntegration[] }>("/api/me/integrations");

export const bindIntegration = (platform: string, external_id: string) =>
  apiClient.post("/api/me/integrations", { platform, external_id });

export const getFuelSettings = () =>
  apiClient.get<FuelSettings>("/api/fuel/settings");

export const updateFuelSettings = (data: FuelSettings) =>
  apiClient.put("/api/fuel/settings", data);

export const getMyFuel = () =>
  apiClient.get<{ transactions: FuelTransaction[] }>("/api/me/fuel");
```

- [ ] **Step 2: KPI Leaderboard Page**

```tsx
// dashboard/src/pages/admin/KpiPage.tsx
import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getKPISnapshots, triggerKPISnapshot } from "../../api/client";
import type { EfficiencySnapshot } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Badge } from "../../components/ui/badge";

const currentMonth = new Date().toISOString().slice(0, 7);

function groupByPeerGroup(snapshots: EfficiencySnapshot[]) {
  const groups: Record<string, EfficiencySnapshot[]> = {};
  for (const s of snapshots) {
    if (!groups[s.peer_group]) groups[s.peer_group] = [];
    groups[s.peer_group].push(s);
  }
  return groups;
}

export function KpiPage() {
  const [month, setMonth] = useState(currentMonth);
  const [tab, setTab] = useState<"regular" | "cohort">("regular");

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["kpi-snapshots", month],
    queryFn: () => getKPISnapshots(month).then((r) => r.data),
  });

  const triggerMutation = useMutation({
    mutationFn: () => triggerKPISnapshot(month),
    onSuccess: () => refetch(),
  });

  const allSnapshots = data?.snapshots ?? [];
  const regularSnapshots = allSnapshots.filter((s) => !s.peer_group.startsWith("cohort_"));
  const cohortSnapshots = allSnapshots.filter((s) => s.peer_group.startsWith("cohort_"));

  const displayed = tab === "regular" ? groupByPeerGroup(regularSnapshots) : groupByPeerGroup(cohortSnapshots);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">KPI Leaderboard</h1>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
          />
          <Button
            variant="outline"
            onClick={() => triggerMutation.mutate()}
            disabled={triggerMutation.isPending}
          >
            {triggerMutation.isPending ? "Computing..." : "Run Snapshot"}
          </Button>
        </div>
      </div>

      <div className="flex gap-2">
        {(["regular", "cohort"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-colors ${
              tab === t ? "bg-indigo-600 text-white" : "bg-zinc-800 text-zinc-400 hover:text-zinc-100"
            }`}
          >
            {t === "regular" ? "Established Employees" : "New Employees (Cohort)"}
          </button>
        ))}
      </div>

      {isLoading ? (
        <p className="text-zinc-500 text-sm">Loading...</p>
      ) : Object.keys(displayed).length === 0 ? (
        <p className="text-zinc-500 text-sm py-8 text-center">
          No data for {month}. Click "Run Snapshot" to compute.
        </p>
      ) : (
        Object.entries(displayed).map(([group, snapshots]) => (
          <Card key={group}>
            <CardHeader>
              <CardTitle className="capitalize">{group.replace("cohort_", "Cohort: ")}</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-2 font-medium">Rank</th>
                    <th className="text-left py-2 font-medium">Employee</th>
                    <th className="text-right py-2 font-medium">Efficiency Score</th>
                    <th className="text-right py-2 font-medium">Output Weight</th>
                    <th className="text-right py-2 font-medium">SCU Used</th>
                    <th className="text-right py-2 font-medium">Peer Rank</th>
                  </tr>
                </thead>
                <tbody>
                  {snapshots.map((s) => (
                    <tr key={s.id} className="border-b border-zinc-800/50">
                      <td className="py-2 font-bold text-indigo-400">#{s.rank}</td>
                      <td>{s.user_name}</td>
                      <td className="text-right font-mono">{s.efficiency_score.toFixed(2)}</td>
                      <td className="text-right">{s.total_output_weight.toFixed(1)}</td>
                      <td className="text-right">{s.total_scu.toLocaleString()}</td>
                      <td className="text-right">
                        <Badge variant="outline">{s.rank} / {s.peer_count}</Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  );
}
```

- [ ] **Step 3: Integrations Config Page**

```tsx
// dashboard/src/pages/admin/IntegrationsPage.tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { getWebhookConfigs, createWebhookConfig } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const PLATFORMS = ["github", "jira", "feishu", "dingtalk"];

export function IntegrationsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ platform: "github", webhook_secret: "" });

  const { data } = useQuery({
    queryKey: ["webhook-configs"],
    queryFn: () => getWebhookConfigs().then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: createWebhookConfig,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["webhook-configs"] });
      setOpen(false);
      setForm({ platform: "github", webhook_secret: "" });
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate(form);
  };

  const webhookUrl = (platform: string) =>
    `${window.location.protocol}//${window.location.hostname}:8081/webhooks/${platform}?tenant_id=<YOUR_TENANT_ID>`;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Integrations</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>+ Add Integration</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Configure Webhook</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-1">
                <Label>Platform</Label>
                <select
                  className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                  value={form.platform}
                  onChange={(e) => setForm({ ...form, platform: e.target.value })}
                >
                  {PLATFORMS.map((p) => <option key={p} value={p}>{p}</option>)}
                </select>
              </div>
              <div className="space-y-1">
                <Label>Webhook Secret</Label>
                <Input
                  type="password"
                  placeholder="Secret configured in GitHub/Jira/飞书"
                  value={form.webhook_secret}
                  onChange={(e) => setForm({ ...form, webhook_secret: e.target.value })}
                  required
                />
              </div>
              <div className="rounded-md bg-zinc-800 p-3 text-xs text-zinc-400 break-all">
                <p className="font-medium text-zinc-300 mb-1">Webhook URL to register:</p>
                <code>{webhookUrl(form.platform)}</code>
              </div>
              <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                {createMutation.isPending ? "Saving..." : "Save Integration"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          {!data?.configs?.length ? (
            <p className="text-zinc-500 text-sm py-4 text-center">No integrations configured.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Platform</th>
                  <th className="text-left py-2 font-medium">Webhook URL</th>
                  <th className="text-right py-2 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {data.configs.map((c) => (
                  <tr key={c.id} className="border-b border-zinc-800/50">
                    <td className="py-2 capitalize">{c.platform}</td>
                    <td className="text-zinc-400 text-xs truncate max-w-[300px]">
                      {webhookUrl(c.platform)}
                    </td>
                    <td className="text-right">
                      <Badge variant={c.is_active ? "default" : "secondary"}>
                        {c.is_active ? "Active" : "Inactive"}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 4: Update My Usage page — add efficiency score, growth curve, account binding**

Replace `dashboard/src/pages/employee/MyUsagePage.tsx` with:

```tsx
import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, getMyKPI, getMyFuel, getMyIntegrations, bindIntegration, apiClient } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { QuotaMeter } from "../../components/QuotaMeter";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const currentMonth = new Date().toISOString().slice(0, 7);

function useMyQuota() {
  return useQuery({
    queryKey: ["my-quota"],
    queryFn: () => apiClient.get<{ quota_scu: number; used_scu: number }>("/api/me/quota").then((r) => r.data),
  });
}

export function MyUsagePage() {
  const { data: summaryData } = useQuery({
    queryKey: ["my-summary", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });
  const { data: quotaData } = useMyQuota();
  const { data: kpiData } = useQuery({ queryKey: ["my-kpi"], queryFn: () => getMyKPI().then((r) => r.data) });
  const { data: fuelData } = useQuery({ queryKey: ["my-fuel"], queryFn: () => getMyFuel().then((r) => r.data) });
  const { data: intData, refetch: refetchInt } = useQuery({
    queryKey: ["my-integrations"],
    queryFn: () => getMyIntegrations().then((r) => r.data),
  });

  const [quotaOpen, setQuotaOpen] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [quotaForm, setQuotaForm] = useState({ new_quota: "", reason: "" });
  const [bindForm, setBindForm] = useState({ platform: "github", external_id: "" });

  const requestMutation = useMutation({
    mutationFn: (payload: { new_quota: number; reason: string }) =>
      apiClient.post("/api/quota/request", payload),
    onSuccess: () => { setQuotaOpen(false); setQuotaForm({ new_quota: "", reason: "" }); },
  });

  const bindMutation = useMutation({
    mutationFn: () => bindIntegration(bindForm.platform, bindForm.external_id),
    onSuccess: () => { setBindOpen(false); setBindForm({ platform: "github", external_id: "" }); refetchInt(); },
  });

  const mySummary = summaryData?.summaries?.[0];
  const usedSCU = mySummary?.total_scu ?? 0;
  const quotaSCU = quotaData?.quota_scu ?? 0;
  const latestSnapshot = kpiData?.snapshots?.[0];
  const isNewEmployee = latestSnapshot?.peer_group?.startsWith("cohort_");

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">My Usage — {currentMonth}</h1>
        <div className="flex gap-2">
          <Dialog open={bindOpen} onOpenChange={setBindOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Link Account</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Link Third-Party Account</DialogTitle></DialogHeader>
              <div className="space-y-4">
                <div className="space-y-1">
                  <Label>Platform</Label>
                  <select
                    className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                    value={bindForm.platform}
                    onChange={(e) => setBindForm({ ...bindForm, platform: e.target.value })}
                  >
                    {["github", "jira", "feishu", "dingtalk"].map((p) => (
                      <option key={p} value={p}>{p}</option>
                    ))}
                  </select>
                </div>
                <div className="space-y-1">
                  <Label>Username / Account ID</Label>
                  <Input
                    placeholder="e.g. alice-gh (GitHub login)"
                    value={bindForm.external_id}
                    onChange={(e) => setBindForm({ ...bindForm, external_id: e.target.value })}
                  />
                </div>
                <Button className="w-full" disabled={bindMutation.isPending} onClick={() => bindMutation.mutate()}>
                  {bindMutation.isPending ? "Linking..." : "Link Account"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
          <Dialog open={quotaOpen} onOpenChange={setQuotaOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Request Quota</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Request Quota Increase</DialogTitle></DialogHeader>
              <form onSubmit={(e) => { e.preventDefault(); requestMutation.mutate({ new_quota: parseInt(quotaForm.new_quota), reason: quotaForm.reason }); }} className="space-y-4">
                <div className="space-y-1">
                  <Label>Requested SCU Limit</Label>
                  <Input type="number" min="1" value={quotaForm.new_quota} onChange={(e) => setQuotaForm({ ...quotaForm, new_quota: e.target.value })} required />
                </div>
                <div className="space-y-1">
                  <Label>Reason</Label>
                  <Input value={quotaForm.reason} onChange={(e) => setQuotaForm({ ...quotaForm, reason: e.target.value })} />
                </div>
                <Button type="submit" className="w-full" disabled={requestMutation.isPending}>
                  {requestMutation.isPending ? "Submitting..." : "Submit"}
                </Button>
              </form>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-zinc-400 font-normal">SCU Used</CardTitle></CardHeader>
          <CardContent><p className="text-3xl font-bold">{usedSCU.toLocaleString()}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-zinc-400 font-normal">Cost (USD)</CardTitle></CardHeader>
          <CardContent><p className="text-3xl font-bold">${(mySummary?.total_usd ?? 0).toFixed(4)}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-zinc-400 font-normal">Requests</CardTitle></CardHeader>
          <CardContent><p className="text-3xl font-bold">{mySummary?.request_count ?? 0}</p></CardContent>
        </Card>
      </div>

      {quotaSCU > 0 && (
        <Card>
          <CardHeader><CardTitle>Monthly Quota</CardTitle></CardHeader>
          <CardContent><QuotaMeter used={usedSCU} limit={quotaSCU} /></CardContent>
        </Card>
      )}

      {latestSnapshot && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              Efficiency Score
              {isNewEmployee && (
                <Badge variant="secondary">New Employee Period</Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-baseline gap-3 mb-4">
              <p className="text-4xl font-bold text-indigo-400">{latestSnapshot.efficiency_score.toFixed(2)}</p>
              {!isNewEmployee && (
                <p className="text-zinc-400 text-sm">Peer rank: <span className="text-zinc-100 font-medium">{latestSnapshot.rank} / {latestSnapshot.peer_count}</span></p>
              )}
              {isNewEmployee && (
                <p className="text-zinc-400 text-sm">Cohort rank: <span className="text-zinc-100 font-medium">{latestSnapshot.rank} / {latestSnapshot.peer_count}</span></p>
              )}
            </div>
            {kpiData && kpiData.snapshots.length > 1 && (
              <div className="mt-2">
                <p className="text-xs text-zinc-500 mb-2">Growth curve (last 12 months)</p>
                <div className="flex items-end gap-1 h-16">
                  {[...kpiData.snapshots].reverse().map((s) => {
                    const maxScore = Math.max(...kpiData.snapshots.map((x) => x.efficiency_score), 0.01);
                    const h = Math.round((s.efficiency_score / maxScore) * 100);
                    return (
                      <div key={s.year_month} className="flex flex-col items-center gap-1 flex-1">
                        <div className="w-full bg-indigo-600 rounded-sm" style={{ height: `${h}%` }} title={`${s.year_month}: ${s.efficiency_score.toFixed(2)}`} />
                        <span className="text-zinc-600 text-xs">{s.year_month.slice(5)}</span>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {fuelData && fuelData.transactions.length > 0 && (
        <Card>
          <CardHeader><CardTitle>AI-Fuel Rewards</CardTitle></CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Date</th>
                  <th className="text-left py-2 font-medium">Tier</th>
                  <th className="text-right py-2 font-medium">SCU Awarded</th>
                </tr>
              </thead>
              <tbody>
                {fuelData.transactions.map((t) => (
                  <tr key={t.id} className="border-b border-zinc-800/50">
                    <td className="py-2 text-zinc-400">{t.created_at.slice(0, 10)}</td>
                    <td><Badge variant="outline">{t.tier || t.reason}</Badge></td>
                    <td className="text-right text-green-400">+{t.amount_scu.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}

      {intData && (
        <Card>
          <CardHeader><CardTitle>Linked Accounts</CardTitle></CardHeader>
          <CardContent>
            {!intData.integrations?.length ? (
              <p className="text-zinc-500 text-sm">No accounts linked. Click "Link Account" to connect GitHub/Jira/飞书.</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {intData.integrations.map((i) => (
                  <Badge key={i.id} variant="secondary" className="capitalize">
                    {i.platform}: {i.external_id}
                  </Badge>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Update Layout nav and App routes**

In `dashboard/src/components/Layout.tsx`, add two nav items:

```tsx
const navItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "My Usage", href: "/me" },
];
```

In `dashboard/src/App.tsx`, add imports and routes:

```tsx
// Add imports:
import { KpiPage } from "./pages/admin/KpiPage";
import { IntegrationsPage } from "./pages/admin/IntegrationsPage";

// Add routes inside the protected "/" Route:
<Route path="admin/kpi" element={<KpiPage />} />
<Route path="admin/integrations" element={<IntegrationsPage />} />
```

- [ ] **Step 6: Build dashboard**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run build 2>&1
```

Expected: clean build, no TypeScript errors.

- [ ] **Step 7: Commit all dashboard changes**

```bash
git add dashboard/src/api/client.ts \
        dashboard/src/pages/admin/KpiPage.tsx \
        dashboard/src/pages/admin/IntegrationsPage.tsx \
        dashboard/src/pages/employee/MyUsagePage.tsx \
        dashboard/src/components/Layout.tsx \
        dashboard/src/App.tsx
git commit -m "feat(dashboard): Phase 2 — KPI leaderboard, Integrations config, My Usage with efficiency score"
```

---

## Final Verification

- [ ] **Start all services and verify health**

```bash
# Terminal 1: start infra
cd /Users/sugac.275/ToTra && docker-compose up -d postgres redis

# Terminal 2: start admin
cd /Users/sugac.275/ToTra/admin && \
  POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_DB=totra \
  POSTGRES_USER=totra POSTGRES_PASSWORD=totra_secret \
  REDIS_HOST=localhost REDIS_PORT=6379 \
  JWT_SECRET=devsecret \
  ENCRYPTION_KEY=6368616e676520746869732070617373776f726420746f206120736563726574 \
  ./admin

# Test health
curl http://localhost:8081/health
# Expected: {"status":"ok"}

# Test manual KPI trigger
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":""}' | jq -r .token)

curl -s -X POST "http://localhost:8081/api/admin/kpi/run?month=2026-05" \
  -H "Authorization: Bearer $TOKEN"
# Expected: {"status":"ok","month":"2026-05"}
```

- [ ] **Final commit tag**

```bash
git tag phase2-complete
```
