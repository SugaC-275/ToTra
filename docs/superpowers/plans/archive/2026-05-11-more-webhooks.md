# GitLab and Confluence Webhook Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitLab and Confluence webhook handlers to the ToTra admin service so those platforms can emit events that are stored, attributed to users, and weighted exactly like GitHub, Jira, and Feishu events.

**Architecture:** Three database CHECK constraints hard-code the allowed platform values and must be widened first (Task 0); the existing `WebhookService.SaveEvent` / `MatchUser` / `GetWebhookConfig` pipeline is reused without modification. Two new handler functions (`gitlabWebhook`, `confluenceWebhook`) are added to `admin/api/webhooks.go`, two new parse functions (`ParseGitLabEvent`, `ParseConfluenceEvent`) and one new auth helper (`VerifyGitLabToken`) are added to `admin/services/webhook.go`, and default weights for both platforms are appended to `defaultWeights`. The React `IntegrationsPage` platform dropdown is updated to expose both new platforms.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, `crypto/hmac` + `crypto/sha256` (stdlib, no new dependencies); React 19 + TypeScript, TanStack Query v5, existing UI components.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `infra/postgres/007_add_gitlab_confluence_platforms.sql` (create) |
| 1 | `admin/services/webhook.go` (modify), `admin/services/webhook_test.go` (modify) |
| 2 | `admin/api/webhooks.go` (modify), `admin/api/webhook_more_test.go` (create) |
| 3 | `dashboard/src/pages/admin/IntegrationsPage.tsx` (modify) |

---

## Task 0: DB migration — widen platform CHECK constraints

**Files:**
- Create: `infra/postgres/007_add_gitlab_confluence_platforms.sql`

**Background:** Three tables in `infra/postgres/003_phase2.sql` carry a hard-coded CHECK constraint listing exactly `('github','jira','feishu','dingtalk')`: `webhook_configs.platform`, `user_integrations.platform`, and `output_events.platform`. Any INSERT with `platform = 'gitlab'` or `platform = 'confluence'` will fail. This migration widens those constraints.

- [ ] **Step 1: Create `infra/postgres/007_add_gitlab_confluence_platforms.sql`**

```sql
BEGIN;

-- webhook_configs
ALTER TABLE webhook_configs
  DROP CONSTRAINT IF EXISTS webhook_configs_platform_check;
ALTER TABLE webhook_configs
  ADD CONSTRAINT webhook_configs_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

-- user_integrations
ALTER TABLE user_integrations
  DROP CONSTRAINT IF EXISTS user_integrations_platform_check;
ALTER TABLE user_integrations
  ADD CONSTRAINT user_integrations_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

-- output_events
ALTER TABLE output_events
  DROP CONSTRAINT IF EXISTS output_events_platform_check;
ALTER TABLE output_events
  ADD CONSTRAINT output_events_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

COMMIT;
```

- [ ] **Step 2: Apply the migration**

```bash
psql "postgres://totra:totra@localhost:5432/totra" \
  -f /Users/sugac.275/ToTra/infra/postgres/007_add_gitlab_confluence_platforms.sql
```

Expected output: `BEGIN`, 6 × `ALTER TABLE`, `COMMIT`.

- [ ] **Step 3: Verify constraints**

```bash
psql "postgres://totra:totra@localhost:5432/totra" -c "
SELECT conrelid::regclass AS table_name, conname, pg_get_constraintdef(oid) AS definition
FROM pg_constraint
WHERE contype = 'c'
  AND conrelid IN (
    'webhook_configs'::regclass,
    'user_integrations'::regclass,
    'output_events'::regclass
  )
  AND conname LIKE '%platform%';
"
```

Expected: all three rows contain `'gitlab'` and `'confluence'`.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add infra/postgres/007_add_gitlab_confluence_platforms.sql
git commit -m "feat(db): widen platform CHECK constraints to include gitlab and confluence"
```

---

## Task 1: Service layer — parse functions, token verifier, default weights

**Files:**
- Modify: `admin/services/webhook.go`
- Modify: `admin/services/webhook_test.go`

Read both files first to understand existing structure before editing.

- [ ] **Step 1: Append failing tests to `admin/services/webhook_test.go`**

Add after the last test function in the file:

```go
// ---- GitLab ----

func TestVerifyGitLabToken(t *testing.T) {
	assert.True(t, services.VerifyGitLabToken("mysecret", "mysecret"))
	assert.False(t, services.VerifyGitLabToken("mysecret", "wrongsecret"))
	assert.False(t, services.VerifyGitLabToken("mysecret", ""))
}

func TestParseGitLabPushEvent(t *testing.T) {
	payload := []byte(`{
		"user_username": "alice-gl",
		"commits": [{"author": {"name": "Alice"}}]
	}`)
	event, err := services.ParseGitLabEvent("Push Hook", payload)
	assert.NoError(t, err)
	assert.Equal(t, "gitlab", event.Platform)
	assert.Equal(t, "push", event.EventType)
	assert.Equal(t, "Alice", event.SenderLogin)
}

func TestParseGitLabPushEvent_FallbackToUserUsername(t *testing.T) {
	payload := []byte(`{
		"user_username": "alice-gl",
		"commits": [{"author": {"name": ""}}]
	}`)
	event, err := services.ParseGitLabEvent("Push Hook", payload)
	assert.NoError(t, err)
	assert.Equal(t, "alice-gl", event.SenderLogin)
}

func TestParseGitLabPushEvent_NoCommits(t *testing.T) {
	payload := []byte(`{"user_username": "alice-gl", "commits": []}`)
	_, err := services.ParseGitLabEvent("Push Hook", payload)
	assert.Error(t, err)
}

func TestParseGitLabMergeRequestEvent(t *testing.T) {
	payload := []byte(`{
		"user": {"username": "bob-gl"},
		"object_attributes": {"action": "merged", "iid": 7, "title": "Fix login"}
	}`)
	event, err := services.ParseGitLabEvent("Merge Request Hook", payload)
	assert.NoError(t, err)
	assert.Equal(t, "gitlab", event.Platform)
	assert.Equal(t, "merge_request", event.EventType)
	assert.Equal(t, "bob-gl", event.SenderLogin)
	assert.Equal(t, "7", event.ExternalEventID)
	assert.Equal(t, "Fix login", event.Title)
}

func TestParseGitLabUnsupportedEvent(t *testing.T) {
	_, err := services.ParseGitLabEvent("Tag Push Hook", []byte(`{}`))
	assert.Error(t, err)
}

// ---- Confluence ----

func TestParseConfluencePageCreated(t *testing.T) {
	payload := []byte(`{
		"page": {
			"id": "page-123",
			"title": "My New Page",
			"createdBy": {"displayName": "Carol"},
			"version": {"by": {"displayName": "Carol"}}
		}
	}`)
	event, err := services.ParseConfluenceEvent("page_created", payload)
	assert.NoError(t, err)
	assert.Equal(t, "confluence", event.Platform)
	assert.Equal(t, "page_created", event.EventType)
	assert.Equal(t, "Carol", event.SenderLogin)
	assert.Equal(t, "page-123", event.ExternalEventID)
	assert.Equal(t, "My New Page", event.Title)
}

func TestParseConfluencePageUpdated(t *testing.T) {
	payload := []byte(`{
		"page": {
			"id": "page-456",
			"title": "Updated Page",
			"createdBy": {"displayName": "Alice"},
			"version": {"by": {"displayName": "Dave"}}
		}
	}`)
	event, err := services.ParseConfluenceEvent("page_updated", payload)
	assert.NoError(t, err)
	assert.Equal(t, "page_updated", event.EventType)
	assert.Equal(t, "Dave", event.SenderLogin)
	assert.Equal(t, "page-456", event.ExternalEventID)
}

func TestParseConfluenceUnsupportedEvent(t *testing.T) {
	_, err := services.ParseConfluenceEvent("space_created", []byte(`{}`))
	assert.Error(t, err)
}

func TestDefaultWeight_GitLabConfluence(t *testing.T) {
	weights := map[string]float64{}
	assert.Equal(t, 1.0, services.EventWeight("gitlab", "push", weights))
	assert.Equal(t, 3.0, services.EventWeight("gitlab", "merge_request", weights))
	assert.Equal(t, 2.0, services.EventWeight("confluence", "page_created", weights))
	assert.Equal(t, 1.0, services.EventWeight("confluence", "page_updated", weights))
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestVerifyGitLab|TestParseGitLab|TestParseConfluence|TestDefaultWeight_GitLabConfluence" -v 2>&1 | head -20
```

Expected: compilation error — functions undefined.

- [ ] **Step 3a: Replace `defaultWeights` in `admin/services/webhook.go`**

Find the existing `defaultWeights` variable (it lists `github`, `jira`, `feishu`, `dingtalk`) and replace it with:

```go
var defaultWeights = map[string]map[string]float64{
	"github":     {"pr_merged": 5, "push": 1},
	"jira":       {"issue_closed": 3},
	"feishu":     {"task_completed": 2, "doc_created": 2},
	"dingtalk":   {"task_completed": 2},
	"gitlab":     {"push": 1, "merge_request": 3},
	"confluence": {"page_created": 2, "page_updated": 1},
}
```

- [ ] **Step 3b: Append three new functions to the end of `admin/services/webhook.go`**

The existing imports already include `"crypto/hmac"`, `"encoding/json"`, `"errors"`, `"fmt"`, and `"time"`. No new imports needed.

```go
// VerifyGitLabToken checks the X-Gitlab-Token header against the stored secret using
// constant-time comparison to prevent timing attacks.
func VerifyGitLabToken(secret, header string) bool {
	return hmac.Equal([]byte(secret), []byte(header))
}

// ParseGitLabEvent parses a GitLab webhook payload.
// Supported X-Gitlab-Event values: "Push Hook", "Merge Request Hook".
func ParseGitLabEvent(eventType string, body []byte) (*ParsedEvent, error) {
	var raw struct {
		UserUsername string `json:"user_username"`
		Commits      []struct {
			Author struct {
				Name string `json:"name"`
			} `json:"author"`
		} `json:"commits"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
		ObjectAttributes struct {
			Action string `json:"action"`
			IID    int    `json:"iid"`
			Title  string `json:"title"`
		} `json:"object_attributes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	switch eventType {
	case "Push Hook":
		if len(raw.Commits) == 0 {
			return nil, errors.New("push event has no commits, skipping")
		}
		actor := raw.Commits[0].Author.Name
		if actor == "" {
			actor = raw.UserUsername
		}
		return &ParsedEvent{
			Platform:    "gitlab",
			EventType:   "push",
			OccurredAt:  time.Now().UTC(),
			SenderLogin: actor,
		}, nil

	case "Merge Request Hook":
		return &ParsedEvent{
			Platform:        "gitlab",
			EventType:       "merge_request",
			ExternalEventID: fmt.Sprintf("%d", raw.ObjectAttributes.IID),
			Title:           raw.ObjectAttributes.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.User.Username,
		}, nil
	}

	return nil, fmt.Errorf("unsupported gitlab event: %s", eventType)
}

// ParseConfluenceEvent parses a Confluence Cloud webhook payload.
// Supported X-Event-Key values: "page_created", "page_updated".
func ParseConfluenceEvent(eventKey string, body []byte) (*ParsedEvent, error) {
	var raw struct {
		Page struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			CreatedBy struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
			Version struct {
				By struct {
					DisplayName string `json:"displayName"`
				} `json:"by"`
			} `json:"version"`
		} `json:"page"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	switch eventKey {
	case "page_created":
		return &ParsedEvent{
			Platform:        "confluence",
			EventType:       "page_created",
			ExternalEventID: raw.Page.ID,
			Title:           raw.Page.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Page.CreatedBy.DisplayName,
		}, nil

	case "page_updated":
		return &ParsedEvent{
			Platform:        "confluence",
			EventType:       "page_updated",
			ExternalEventID: raw.Page.ID,
			Title:           raw.Page.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Page.Version.By.DisplayName,
		}, nil
	}

	return nil, fmt.Errorf("unsupported confluence event: %s", eventKey)
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestVerifyGitLab|TestParseGitLab|TestParseConfluence|TestDefaultWeight_GitLabConfluence" -v
```

Expected: all 10 new tests PASS.

- [ ] **Step 5: Run all service tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -v
```

Expected: all prior tests still pass.

- [ ] **Step 6: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/services/webhook.go admin/services/webhook_test.go
git commit -m "feat(services): add GitLab and Confluence parse functions and default weights"
```

---

## Task 2: API handlers — `gitlabWebhook` and `confluenceWebhook`

**Files:**
- Modify: `admin/api/webhooks.go`
- Create: `admin/api/webhook_more_test.go`

Read `admin/api/webhooks.go` first to understand the existing handler structure before replacing it.

- [ ] **Step 1: Create `admin/api/webhook_more_test.go`**

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

const testEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func encryptForTest(t *testing.T, plaintext string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plaintext, testEncKey)
	require.NoError(t, err)
	return enc
}

type stubWebhookSvc struct {
	encryptedSecret string
	weights         map[string]float64
	cfgErr          error
	saveErr         error
}

func (s *stubWebhookSvc) GetWebhookConfig(_ context.Context, _, _ string) (string, map[string]float64, error) {
	return s.encryptedSecret, s.weights, s.cfgErr
}
func (s *stubWebhookSvc) MatchUser(_ context.Context, _ string, _ *services.ParsedEvent) (string, error) {
	return "", nil
}
func (s *stubWebhookSvc) SaveEvent(_ context.Context, _, _ string, _ *services.ParsedEvent, _ []byte) error {
	return s.saveErr
}

func setupWebhookTestApp(svc *stubWebhookSvc) *fiber.App {
	app := fiber.New()
	api.RegisterWebhookRoutes(app, svc, testEncKey)
	return app
}

func TestGitLabWebhook_MissingTenantID(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGitLabWebhook_NotConfigured(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{cfgErr: errors.New("not found")})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestGitLabWebhook_InvalidToken(t *testing.T) {
	enc := encryptForTest(t, "correct-token")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user_username":"alice","commits":[{"author":{"name":"Alice"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "wrong-token")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestGitLabWebhook_PushHook_OK(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user_username":"alice","commits":[{"author":{"name":"Alice"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestGitLabWebhook_MergeRequestHook_OK(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user":{"username":"bob"},"object_attributes":{"action":"merged","iid":7,"title":"Fix login"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestGitLabWebhook_UnsupportedEvent_Skipped(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Tag Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "skipped", result["status"])
}

func TestConfluenceWebhook_MissingTenantID(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestConfluenceWebhook_NotConfigured(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{cfgErr: errors.New("not found")})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestConfluenceWebhook_InvalidSignature(t *testing.T) {
	enc := encryptForTest(t, "correct-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p1","title":"T","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Alice"}}}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", "sha256=invalidsig")
	req.Header.Set("X-Event-Key", "page_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestConfluenceWebhook_PageCreated_OK(t *testing.T) {
	secret := "conf-secret-123"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p1","title":"My Page","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Alice"}}}}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "page_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestConfluenceWebhook_PageUpdated_OK(t *testing.T) {
	secret := "conf-secret-456"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p2","title":"Updated","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Bob"}}}}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "page_updated")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestConfluenceWebhook_UnsupportedEvent_Skipped(t *testing.T) {
	secret := "conf-secret-789"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "space_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "skipped", result["status"])
}
```

**NOTE:** The test file uses `testEncKey` and `encryptForTest` — these must NOT conflict with any same-named identifiers in existing `api_test` package files. Check if `integration_test.go` defines a `testEncKey` constant or `encryptForTest` function — if it does, rename these in `webhook_more_test.go` to `webhookTestEncKey` and `webhookEncryptForTest`.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGitLab|TestConfluence" -v 2>&1 | head -20
```

Expected: compilation error.

- [ ] **Step 3: Replace the full content of `admin/api/webhooks.go`**

Read the existing file first, then replace with:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

type webhookServiceIface interface {
	GetWebhookConfig(ctx context.Context, tenantID, platform string) (string, map[string]float64, error)
	MatchUser(ctx context.Context, tenantID string, event *services.ParsedEvent) (string, error)
	SaveEvent(ctx context.Context, tenantID, userID string, event *services.ParsedEvent, rawPayload []byte) error
}

func RegisterWebhookRoutes(app fiber.Router, svc webhookServiceIface, encryptionKey string) {
	app.Post("/webhooks/github", githubWebhook(svc, encryptionKey))
	app.Post("/webhooks/jira", jiraWebhook(svc, encryptionKey))
	app.Post("/webhooks/feishu", feishuWebhook(svc, encryptionKey))
	app.Post("/webhooks/gitlab", gitlabWebhook(svc, encryptionKey))
	app.Post("/webhooks/confluence", confluenceWebhook(svc, encryptionKey))
}

func githubWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
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

func jiraWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
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

func feishuWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
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

func gitlabWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "gitlab")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "gitlab integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitLabToken(secret, c.Get("X-Gitlab-Token")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid token"})
		}
		eventType := c.Get("X-Gitlab-Event")
		event, err := services.ParseGitLabEvent(eventType, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("gitlab", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}

func confluenceWebhook(svc webhookServiceIface, encKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		body := c.Body()
		encSecret, weights, err := svc.GetWebhookConfig(c.Context(), tenantID, "confluence")
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "confluence integration not configured"})
		}
		secret, err := crypto.Decrypt(encSecret, encKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "config error"})
		}
		if !services.VerifyGitHubSignature(body, secret, c.Get("X-Hub-Signature")) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
		}
		eventKey := c.Get("X-Event-Key")
		event, err := services.ParseConfluenceEvent(eventKey, body)
		if err != nil {
			return c.Status(200).JSON(fiber.Map{"status": "skipped", "reason": err.Error()})
		}
		event.Weight = services.EventWeight("confluence", event.EventType, weights)
		userID, _ := svc.MatchUser(c.Context(), tenantID, event)
		if err := svc.SaveEvent(c.Context(), tenantID, userID, event, body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"status": "ok"})
	}
}
```

- [ ] **Step 4: Run new handler tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGitLab|TestConfluence" -v
```

Expected: all 12 tests PASS.

- [ ] **Step 5: Run all API tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -v
```

Expected: all pre-existing tests still pass.

- [ ] **Step 6: Build**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/api/webhooks.go admin/api/webhook_more_test.go
git commit -m "feat(api): add GitLab and Confluence webhook handlers"
```

---

## Task 3: Dashboard UI — add GitLab and Confluence to IntegrationsPage

**Files:**
- Modify: `dashboard/src/pages/admin/IntegrationsPage.tsx`

Read the file first. Two targeted changes only:
1. The platform `<select>` options array — add `"gitlab"` and `"confluence"`
2. The `<Input>` placeholder — mention GitLab and Confluence

- [ ] **Step 1: Update the platform select options**

Find the `<select>` element with the platforms array. The current array is `["github", "jira", "feishu", "dingtalk"]`. Replace it with:

```
["github", "jira", "feishu", "dingtalk", "gitlab", "confluence"]
```

- [ ] **Step 2: Update the Input placeholder**

Find the webhook secret `<Input>` placeholder string. Update it to include GitLab and Confluence:

```
Secret configured in GitHub / Jira / 飞书 / GitLab / Confluence
```

- [ ] **Step 3: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add dashboard/src/pages/admin/IntegrationsPage.tsx
git commit -m "feat(dashboard): add GitLab and Confluence to integrations platform dropdown"
```
