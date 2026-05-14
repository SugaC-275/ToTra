# GDPR & Compliance (Plan J) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GDPR-compliant data governance to ToTra: per-tenant configurable data retention with manual/scheduled cleanup, employee self-service data export (JSON), and a data deletion request workflow (employee requests → admin approves/rejects with actual erasure).

**Architecture:** Two new services (`DataRetentionService`, `DeletionRequestService`) backed by a single migration adding `data_retention_months` to tenants and a new `data_deletion_requests` table. Six new API endpoints (three admin, two employee, one employee GET). A new `GDPRPage.tsx` for admins and two new buttons on `MyUsagePage.tsx` for employees.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, React 19 + TypeScript, TanStack Query v5, Tailwind v4.

---

## File Map

| Task | Files |
|------|-------|
| 0 | `infra/postgres/010_gdpr_compliance.sql` (create) |
| 1 | `admin/services/data_retention.go` (create), `admin/services/data_retention_test.go` (create) |
| 2 | `admin/services/deletion_request.go` (create), `admin/services/deletion_request_test.go` (create), `admin/api/gdpr.go` (create), `admin/api/gdpr_test.go` (create), `admin/main.go` (modify) |
| 3 | `dashboard/src/api/client.ts` (modify), `dashboard/src/pages/admin/GDPRPage.tsx` (create), `dashboard/src/pages/employee/MyUsagePage.tsx` (modify), `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 0: DB migration — data_retention_months + data_deletion_requests

**Files:**
- Create: `infra/postgres/010_gdpr_compliance.sql`

- [ ] **Step 1: Create the migration file**

```sql
BEGIN;

-- Add configurable retention window to each tenant (default 24 months)
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS data_retention_months INT NOT NULL DEFAULT 24;

-- Deletion requests submitted by employees
CREATE TABLE IF NOT EXISTS data_deletion_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_deletion_requests_tenant_status
    ON data_deletion_requests (tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_deletion_requests_user
    ON data_deletion_requests (user_id);

COMMIT;
```

- [ ] **Step 2: Apply migration**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra < /Users/sugac.275/ToTra/infra/postgres/010_gdpr_compliance.sql
```

Expected: `BEGIN`, `ALTER TABLE`, `CREATE TABLE`, `CREATE INDEX`, `CREATE INDEX`, `COMMIT`.

- [ ] **Step 3: Verify**

```bash
docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra -c "\d tenants" | grep retention

docker compose -f /Users/sugac.275/ToTra/docker-compose.yml exec -T postgres \
  psql -U totra -d totra -c "\d data_deletion_requests"
```

Expected: `data_retention_months` column on tenants; `data_deletion_requests` table with id, tenant_id, user_id, status, requested_at, processed_at.

- [ ] **Step 4: Commit**

```bash
cd /Users/sugac.275/ToTra && git add infra/postgres/010_gdpr_compliance.sql
git commit -m "feat(db): add data_retention_months to tenants and data_deletion_requests table"
```

---

## Task 1: Service layer — DataRetentionService

**Files:**
- Create: `admin/services/data_retention.go`
- Create: `admin/services/data_retention_test.go`

- [ ] **Step 1: Write `admin/services/data_retention_test.go`**

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

func TestRetentionCutoff_24Months(t *testing.T) {
	// RetentionCutoff is a pure function; easy to unit-test without DB
	// We just check it returns a non-zero time earlier than now
	import "time"
	cutoff := services.RetentionCutoff(24)
	assert.True(t, cutoff.Before(time.Now()))
	// Should be roughly 24 months ago (within 1 day tolerance)
	expected := time.Now().AddDate(-2, 0, 0)
	diff := cutoff.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff.Hours(), 25.0) // within 25 hours
}

func TestRetentionCutoff_0Months(t *testing.T) {
	import "time"
	cutoff := services.RetentionCutoff(0)
	// 0 months means keep nothing older than now — cutoff should be ~now
	assert.WithinDuration(t, time.Now(), cutoff, 5*time.Second)
}
```

Wait — Go doesn't allow import statements inside function bodies. Write the test file properly:

- [ ] **Step 1: Write `admin/services/data_retention_test.go`** (correct version)

```go
package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

func TestRetentionCutoff_24Months(t *testing.T) {
	cutoff := services.RetentionCutoff(24)
	assert.True(t, cutoff.Before(time.Now()))
	expected := time.Now().AddDate(-2, 0, 0)
	diff := cutoff.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff.Hours(), 25.0)
}

func TestRetentionCutoff_0Months(t *testing.T) {
	cutoff := services.RetentionCutoff(0)
	assert.WithinDuration(t, time.Now(), cutoff, 5*time.Second)
}

func TestRetentionCutoff_1Month(t *testing.T) {
	cutoff := services.RetentionCutoff(1)
	expected := time.Now().AddDate(0, -1, 0)
	diff := cutoff.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff.Hours(), 25.0)
}
```

- [ ] **Step 2: Run tests to confirm compilation fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestRetentionCutoff" -v 2>&1 | head -10
```

Expected: compilation error (`RetentionCutoff` not defined).

- [ ] **Step 3: Create `admin/services/data_retention.go`**

```go
package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionCutoff returns the time before which records should be deleted
// when retaining data for the given number of months.
func RetentionCutoff(months int) time.Time {
	return time.Now().UTC().AddDate(0, -months, 0)
}

// DataRetentionServiceIface is the interface used by the API layer.
type DataRetentionServiceIface interface {
	GetRetentionMonths(ctx context.Context, tenantID string) (int, error)
	SetRetentionMonths(ctx context.Context, tenantID string, months int) error
	RunRetentionCleanup(ctx context.Context, tenantID string) (int64, error)
}

// DataRetentionService implements DataRetentionServiceIface.
type DataRetentionService struct {
	pool *pgxpool.Pool
}

func NewDataRetentionService(pool *pgxpool.Pool) *DataRetentionService {
	return &DataRetentionService{pool: pool}
}

// GetRetentionMonths returns the tenant's configured retention window in months.
func (s *DataRetentionService) GetRetentionMonths(ctx context.Context, tenantID string) (int, error) {
	var months int
	err := s.pool.QueryRow(ctx,
		`SELECT data_retention_months FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&months)
	return months, err
}

// SetRetentionMonths updates the tenant's retention window.
// months must be >= 1.
func (s *DataRetentionService) SetRetentionMonths(ctx context.Context, tenantID string, months int) error {
	if months < 1 {
		months = 1
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants SET data_retention_months = $1 WHERE id = $2`,
		months, tenantID,
	)
	return err
}

// RunRetentionCleanup deletes usage_records and output_events older than the
// tenant's retention window. Returns the total number of rows deleted.
func (s *DataRetentionService) RunRetentionCleanup(ctx context.Context, tenantID string) (int64, error) {
	months, err := s.GetRetentionMonths(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	cutoff := RetentionCutoff(months)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var totalDeleted int64

	// Delete old usage_records for this tenant
	tag, err := tx.Exec(ctx,
		`DELETE FROM usage_records WHERE tenant_id = $1 AND request_at < $2`,
		tenantID, cutoff,
	)
	if err != nil {
		return 0, err
	}
	totalDeleted += tag.RowsAffected()

	// Delete old output_events for this tenant (if table exists — guarded by a
	// DO block so the migration is safe even if output_events is not yet present)
	var outputEventsExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'output_events'
		)`,
	).Scan(&outputEventsExists)
	if err != nil {
		return 0, err
	}
	if outputEventsExists {
		tag, err = tx.Exec(ctx,
			`DELETE FROM output_events WHERE tenant_id = $1 AND created_at < $2`,
			tenantID, cutoff,
		)
		if err != nil {
			return 0, err
		}
		totalDeleted += tag.RowsAffected()
	}

	return totalDeleted, tx.Commit(ctx)
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestRetentionCutoff" -v
```

Expected:
```
--- PASS: TestRetentionCutoff_24Months
--- PASS: TestRetentionCutoff_0Months
--- PASS: TestRetentionCutoff_1Month
PASS
```

- [ ] **Step 5: Commit**

```bash
cd /Users/sugac.275/ToTra && git add admin/services/data_retention.go admin/services/data_retention_test.go
git commit -m "feat(services): add DataRetentionService with configurable window and cleanup"
```

---

## Task 2: DeletionRequestService + all GDPR API endpoints + wire main.go

**Files:**
- Create: `admin/services/deletion_request.go`
- Create: `admin/services/deletion_request_test.go`
- Create: `admin/api/gdpr.go`
- Create: `admin/api/gdpr_test.go`
- Modify: `admin/main.go`

### 2a — DeletionRequestService

- [ ] **Step 1: Write `admin/services/deletion_request_test.go`**

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

func TestValidateDeletionStatus_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateDeletionStatus("pending"))
	assert.NoError(t, services.ValidateDeletionStatus("approved"))
	assert.NoError(t, services.ValidateDeletionStatus("rejected"))
}

func TestValidateDeletionStatus_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateDeletionStatus("deleted"))
	assert.Error(t, services.ValidateDeletionStatus(""))
	assert.Error(t, services.ValidateDeletionStatus("PENDING"))
}
```

- [ ] **Step 2: Run tests to confirm compilation fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestValidateDeletionStatus" -v 2>&1 | head -10
```

Expected: compilation error (`ValidateDeletionStatus` not defined).

- [ ] **Step 3: Create `admin/services/deletion_request.go`**

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidateDeletionStatus ensures only the three allowed statuses are used.
func ValidateDeletionStatus(s string) error {
	switch s {
	case "pending", "approved", "rejected":
		return nil
	}
	return fmt.Errorf("invalid deletion request status: %q", s)
}

// DeletionRequest is a GDPR erasure request created by an employee.
type DeletionRequest struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	UserID      string  `json:"user_id"`
	UserName    string  `json:"user_name,omitempty"`
	UserEmail   string  `json:"user_email,omitempty"`
	Status      string  `json:"status"`
	RequestedAt string  `json:"requested_at"`
	ProcessedAt *string `json:"processed_at"`
}

// DataExport is the JSON payload returned to an employee for /api/me/data-export.
type DataExport struct {
	ExportedAt        string               `json:"exported_at"`
	UserID            string               `json:"user_id"`
	UsageRecords      []ExportUsageRecord  `json:"usage_records"`
	EfficiencyHistory []ExportSnapshot     `json:"efficiency_history"`
}

// ExportUsageRecord is a privacy-safe summary row (no model keys, no raw content).
type ExportUsageRecord struct {
	RequestAt        string  `json:"request_at"`
	ModelName        string  `json:"model_name"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	SCUCost          float64 `json:"scu_cost"`
	USDCost          float64 `json:"usd_cost"`
	ResponseMS       int     `json:"response_ms"`
}

// ExportSnapshot is a single monthly efficiency snapshot row.
type ExportSnapshot struct {
	YearMonth       string  `json:"year_month"`
	EfficiencyScore float64 `json:"efficiency_score"`
	AIQScore        float64 `json:"aiq_score"`
	OSSScore        float64 `json:"oss_score"`
	GTSScore        float64 `json:"gts_score"`
	Rank            int     `json:"rank"`
	PeerCount       int     `json:"peer_count"`
}

// DeletionRequestServiceIface is the contract used by the API layer.
type DeletionRequestServiceIface interface {
	CreateRequest(ctx context.Context, tenantID, userID string) (*DeletionRequest, error)
	ListPending(ctx context.Context, tenantID string) ([]*DeletionRequest, error)
	Approve(ctx context.Context, tenantID, requestID string) error
	Reject(ctx context.Context, tenantID, requestID string) error
	ExportUserData(ctx context.Context, tenantID, userID string) (*DataExport, error)
}

// DeletionRequestService implements DeletionRequestServiceIface.
type DeletionRequestService struct {
	pool *pgxpool.Pool
}

func NewDeletionRequestService(pool *pgxpool.Pool) *DeletionRequestService {
	return &DeletionRequestService{pool: pool}
}

// CreateRequest inserts a new pending deletion request.
// Returns an error if the user already has a pending request.
func (s *DeletionRequestService) CreateRequest(ctx context.Context, tenantID, userID string) (*DeletionRequest, error) {
	// Check for existing pending request
	var existingID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM data_deletion_requests
		 WHERE tenant_id = $1 AND user_id = $2 AND status = 'pending'`,
		tenantID, userID,
	).Scan(&existingID)
	if err == nil {
		return nil, errors.New("a pending data deletion request already exists")
	}
	// pgx returns pgx.ErrNoRows when nothing found — that's the happy path

	id := uuid.New().String()
	_, err = s.pool.Exec(ctx,
		`INSERT INTO data_deletion_requests (id, tenant_id, user_id, status)
		 VALUES ($1, $2, $3, 'pending')`,
		id, tenantID, userID,
	)
	if err != nil {
		return nil, err
	}

	req := &DeletionRequest{
		ID:          id,
		TenantID:    tenantID,
		UserID:      userID,
		Status:      "pending",
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return req, nil
}

// ListPending returns all pending deletion requests for the tenant, enriched
// with user name and email for the admin view.
func (s *DeletionRequestService) ListPending(ctx context.Context, tenantID string) ([]*DeletionRequest, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.tenant_id, d.user_id,
		       u.name, u.email,
		       d.status,
		       d.requested_at,
		       d.processed_at
		FROM data_deletion_requests d
		JOIN users u ON u.id = d.user_id
		WHERE d.tenant_id = $1 AND d.status = 'pending'
		ORDER BY d.requested_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DeletionRequest
	for rows.Next() {
		r := &DeletionRequest{}
		var requestedAt time.Time
		var processedAt *time.Time
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.UserID,
			&r.UserName, &r.UserEmail,
			&r.Status,
			&requestedAt, &processedAt,
		); err != nil {
			return nil, err
		}
		r.RequestedAt = requestedAt.Format(time.RFC3339)
		if processedAt != nil {
			s := processedAt.Format(time.RFC3339)
			r.ProcessedAt = &s
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// Approve deletes the user's usage_records and efficiency_snapshots, then
// marks the request approved. Runs in a single transaction.
func (s *DeletionRequestService) Approve(ctx context.Context, tenantID, requestID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Fetch the user_id from the pending request
	var userID string
	err = tx.QueryRow(ctx,
		`UPDATE data_deletion_requests
		 SET status = 'approved', processed_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND status = 'pending'
		 RETURNING user_id`,
		requestID, tenantID,
	).Scan(&userID)
	if err != nil {
		return fmt.Errorf("approve deletion request: %w", err)
	}

	// Erase usage_records
	if _, err = tx.Exec(ctx,
		`DELETE FROM usage_records WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		return err
	}

	// Erase efficiency_snapshots
	if _, err = tx.Exec(ctx,
		`DELETE FROM efficiency_snapshots WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		return err
	}

	// Also clear monthly_summaries
	if _, err = tx.Exec(ctx,
		`DELETE FROM monthly_summaries WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Reject marks the request rejected without deleting any data.
func (s *DeletionRequestService) Reject(ctx context.Context, tenantID, requestID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE data_deletion_requests
		 SET status = 'rejected', processed_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND status = 'pending'`,
		requestID, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("deletion request not found or already processed")
	}
	return nil
}

// ExportUserData returns the past 12 months of usage_records and efficiency_snapshots
// for the given user as a DataExport struct.
func (s *DeletionRequestService) ExportUserData(ctx context.Context, tenantID, userID string) (*DataExport, error) {
	cutoff := time.Now().UTC().AddDate(0, -12, 0)

	// usage_records
	rows, err := s.pool.Query(ctx, `
		SELECT r.request_at, mc.name,
		       r.prompt_tokens, r.completion_tokens,
		       r.scu_cost, r.usd_cost, r.response_ms
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.tenant_id = $1 AND r.user_id = $2
		  AND r.request_at >= $3
		ORDER BY r.request_at DESC`,
		tenantID, userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usageRecords []ExportUsageRecord
	for rows.Next() {
		var rec ExportUsageRecord
		var requestAt time.Time
		if err := rows.Scan(
			&requestAt, &rec.ModelName,
			&rec.PromptTokens, &rec.CompletionTokens,
			&rec.SCUCost, &rec.USDCost, &rec.ResponseMS,
		); err != nil {
			return nil, err
		}
		rec.RequestAt = requestAt.Format(time.RFC3339)
		usageRecords = append(usageRecords, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// efficiency_snapshots (last 12 months)
	snapshotRows, err := s.pool.Query(ctx, `
		SELECT year_month, efficiency_score, aiq_score, oss_score, gts_score, rank, peer_count
		FROM efficiency_snapshots
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY year_month DESC
		LIMIT 12`,
		tenantID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer snapshotRows.Close()

	var snapshots []ExportSnapshot
	for snapshotRows.Next() {
		var snap ExportSnapshot
		if err := snapshotRows.Scan(
			&snap.YearMonth, &snap.EfficiencyScore,
			&snap.AIQScore, &snap.OSSScore, &snap.GTSScore,
			&snap.Rank, &snap.PeerCount,
		); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	if err := snapshotRows.Err(); err != nil {
		return nil, err
	}

	return &DataExport{
		ExportedAt:        time.Now().UTC().Format(time.RFC3339),
		UserID:            userID,
		UsageRecords:      usageRecords,
		EfficiencyHistory: snapshots,
	}, nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run "TestValidateDeletionStatus" -v
```

Expected:
```
--- PASS: TestValidateDeletionStatus_Valid
--- PASS: TestValidateDeletionStatus_Invalid
PASS
```

### 2b — GDPR API handlers

- [ ] **Step 5: Write `admin/api/gdpr_test.go`**

```go
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

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

// ---- stub implementations ----

type stubRetentionSvc struct {
	months int
	err    error
	cleaned int64
}

func (s *stubRetentionSvc) GetRetentionMonths(_ context.Context, _ string) (int, error) {
	return s.months, s.err
}
func (s *stubRetentionSvc) SetRetentionMonths(_ context.Context, _ string, months int) error {
	s.months = months
	return s.err
}
func (s *stubRetentionSvc) RunRetentionCleanup(_ context.Context, _ string) (int64, error) {
	return s.cleaned, s.err
}

type stubDeletionSvc struct {
	req     *services.DeletionRequest
	reqs    []*services.DeletionRequest
	export  *services.DataExport
	err     error
}

func (s *stubDeletionSvc) CreateRequest(_ context.Context, _, _ string) (*services.DeletionRequest, error) {
	return s.req, s.err
}
func (s *stubDeletionSvc) ListPending(_ context.Context, _ string) ([]*services.DeletionRequest, error) {
	return s.reqs, s.err
}
func (s *stubDeletionSvc) Approve(_ context.Context, _, _ string) error { return s.err }
func (s *stubDeletionSvc) Reject(_ context.Context, _, _ string) error  { return s.err }
func (s *stubDeletionSvc) ExportUserData(_ context.Context, _, _ string) (*services.DataExport, error) {
	return s.export, s.err
}

// ---- helpers ----

func setupGDPRApp(retSvc *stubRetentionSvc, delSvc *stubDeletionSvc, role string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid-1", TenantID: "tid-1", Role: role})
		return c.Next()
	})
	api.RegisterGDPRRoutes(app, retSvc, delSvc)
	return app
}

// ---- retention tests ----

func TestGetDataRetention_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 18}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(18), body["data_retention_months"])
}

func TestGetDataRetention_NonAdmin_Forbidden(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 24}, &stubDeletionSvc{}, "standard")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-retention", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestPutDataRetention_Admin(t *testing.T) {
	svc := &stubRetentionSvc{months: 24}
	app := setupGDPRApp(svc, &stubDeletionSvc{}, "admin")
	body, _ := json.Marshal(map[string]int{"data_retention_months": 12})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 12, svc.months)
}

func TestPutDataRetention_InvalidMonths(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{months: 24}, &stubDeletionSvc{}, "admin")
	body, _ := json.Marshal(map[string]int{"data_retention_months": 0})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/data-retention", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestRunRetentionCleanup_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{cleaned: 42}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-retention/run", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(42), body["deleted_count"])
}

// ---- deletion request tests ----

func TestListDeletionRequests_Admin(t *testing.T) {
	reqs := []*services.DeletionRequest{
		{ID: "r1", UserID: "u1", UserName: "Alice", Status: "pending", RequestedAt: "2026-05-01T00:00:00Z"},
	}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{reqs: reqs}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/data-deletion-requests", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	list := body["requests"].([]interface{})
	assert.Len(t, list, 1)
}

func TestApproveDeletionRequest_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestRejectDeletionRequest_Admin(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/reject", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestApproveDeletionRequest_NonAdmin_Forbidden(t *testing.T) {
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{}, "standard")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/data-deletion-requests/r1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---- employee endpoints ----

func TestCreateDeletionRequest_Employee(t *testing.T) {
	dr := &services.DeletionRequest{ID: "r2", UserID: "uid-1", Status: "pending", RequestedAt: "2026-05-11T00:00:00Z"}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{req: dr}, "standard")
	req := httptest.NewRequest(http.MethodPost, "/api/me/data-deletion-request", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}

func TestExportMyData_Employee(t *testing.T) {
	export := &services.DataExport{
		ExportedAt: "2026-05-11T00:00:00Z",
		UserID:     "uid-1",
	}
	app := setupGDPRApp(&stubRetentionSvc{}, &stubDeletionSvc{export: export}, "standard")
	req := httptest.NewRequest(http.MethodGet, "/api/me/data-export", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "uid-1", body["user_id"])
}
```

- [ ] **Step 6: Run tests to confirm compilation fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGetDataRetention|TestPutDataRetention|TestRunRetention|TestListDeletion|TestApproveDeletion|TestRejectDeletion|TestCreateDeletion|TestExportMy" -v 2>&1 | head -15
```

Expected: compilation error (`RegisterGDPRRoutes` not defined).

- [ ] **Step 7: Create `admin/api/gdpr.go`**

```go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// RegisterGDPRRoutes wires all GDPR/compliance endpoints onto the router.
// retSvc and delSvc must already be constructed (passed from main.go).
func RegisterGDPRRoutes(
	app fiber.Router,
	retSvc services.DataRetentionServiceIface,
	delSvc services.DeletionRequestServiceIface,
) {
	// Admin: data retention policy
	app.Get("/api/admin/data-retention", getDataRetention(retSvc))
	app.Put("/api/admin/data-retention", putDataRetention(retSvc))
	app.Post("/api/admin/data-retention/run", runRetentionCleanup(retSvc))

	// Admin: deletion request management
	app.Get("/api/admin/data-deletion-requests", listDeletionRequests(delSvc))
	app.Post("/api/admin/data-deletion-requests/:id/approve", approveDeletionRequest(delSvc))
	app.Post("/api/admin/data-deletion-requests/:id/reject", rejectDeletionRequest(delSvc))

	// Employee: self-service
	app.Get("/api/me/data-export", exportMyData(delSvc))
	app.Post("/api/me/data-deletion-request", createDeletionRequest(delSvc))
}

// --- Admin: data retention ---

func getDataRetention(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		months, err := svc.GetRetentionMonths(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"data_retention_months": months})
	}
}

func putDataRetention(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Months int `json:"data_retention_months"`
		}
		if err := c.BodyParser(&body); err != nil || body.Months < 1 {
			return c.Status(400).JSON(fiber.Map{"error": "data_retention_months must be >= 1"})
		}
		if err := svc.SetRetentionMonths(c.Context(), claims.TenantID, body.Months); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"data_retention_months": body.Months})
	}
}

func runRetentionCleanup(svc services.DataRetentionServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		deleted, err := svc.RunRetentionCleanup(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"deleted_count": deleted})
	}
}

// --- Admin: deletion request management ---

func listDeletionRequests(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		reqs, err := svc.ListPending(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if reqs == nil {
			reqs = []*services.DeletionRequest{}
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

func approveDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Approve(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "approved"})
	}
}

func rejectDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		if err := svc.Reject(c.Context(), claims.TenantID, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "rejected"})
	}
}

// --- Employee: self-service ---

func exportMyData(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		export, err := svc.ExportUserData(c.Context(), claims.TenantID, claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(export)
	}
}

func createDeletionRequest(svc services.DeletionRequestServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		req, err := svc.CreateRequest(c.Context(), claims.TenantID, claims.UserID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(req)
	}
}
```

- [ ] **Step 8: Run tests — expect pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run "TestGetDataRetention|TestPutDataRetention|TestRunRetention|TestListDeletion|TestApproveDeletion|TestRejectDeletion|TestCreateDeletion|TestExportMy" -v
```

Expected: all 10 tests pass.

### 2c — Wire into main.go

- [ ] **Step 9: Modify `admin/main.go`**

Add the two new service instantiations and register the routes. Insert after the `roiSvc` line:

```go
	retentionSvc := services.NewDataRetentionService(pool)
	deletionSvc := services.NewDeletionRequestService(pool)
```

Add after `api.RegisterROIRoutes(protected, roiSvc)`:

```go
	api.RegisterGDPRRoutes(protected, retentionSvc, deletionSvc)
```

The final relevant section of `main.go` should read:

```go
	hrSyncSvc := services.NewHRSyncService(pool)
	roiSvc := services.NewROIService(pool)
	retentionSvc := services.NewDataRetentionService(pool)
	deletionSvc := services.NewDeletionRequestService(pool)

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
	api.RegisterGDPRRoutes(protected, retentionSvc, deletionSvc)
```

- [ ] **Step 10: Verify compilation**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no errors.

- [ ] **Step 11: Run all admin tests**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... -timeout 30s
```

Expected: all existing tests pass, new GDPR tests pass, no regressions.

- [ ] **Step 12: Commit**

```bash
cd /Users/sugac.275/ToTra && \
  git add admin/services/deletion_request.go admin/services/deletion_request_test.go \
          admin/api/gdpr.go admin/api/gdpr_test.go admin/main.go
git commit -m "feat(gdpr): add DeletionRequestService, GDPR API handlers, wire main.go"
```

---

## Task 3: Frontend — GDPRPage + MyUsagePage changes + routing

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Create: `dashboard/src/pages/admin/GDPRPage.tsx`
- Modify: `dashboard/src/pages/employee/MyUsagePage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

### 3a — API client additions

- [ ] **Step 1: Append to `dashboard/src/api/client.ts`**

Add these types and functions at the end of the file:

```typescript
// ---- GDPR & Compliance ----

export interface DeletionRequest {
  id: string;
  tenant_id: string;
  user_id: string;
  user_name?: string;
  user_email?: string;
  status: string;
  requested_at: string;
  processed_at?: string | null;
}

export interface ExportUsageRecord {
  request_at: string;
  model_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  scu_cost: number;
  usd_cost: number;
  response_ms: number;
}

export interface ExportSnapshot {
  year_month: string;
  efficiency_score: number;
  aiq_score: number;
  oss_score: number;
  gts_score: number;
  rank: number;
  peer_count: number;
}

export interface DataExport {
  exported_at: string;
  user_id: string;
  usage_records: ExportUsageRecord[];
  efficiency_history: ExportSnapshot[];
}

export const getDataRetention = () =>
  apiClient.get<{ data_retention_months: number }>("/api/admin/data-retention");

export const setDataRetention = (months: number) =>
  apiClient.put<{ data_retention_months: number }>("/api/admin/data-retention", {
    data_retention_months: months,
  });

export const runRetentionCleanup = () =>
  apiClient.post<{ deleted_count: number }>("/api/admin/data-retention/run");

export const listDeletionRequests = () =>
  apiClient.get<{ requests: DeletionRequest[] }>("/api/admin/data-deletion-requests");

export const approveDeletionRequest = (id: string) =>
  apiClient.post<{ status: string }>(`/api/admin/data-deletion-requests/${id}/approve`);

export const rejectDeletionRequest = (id: string) =>
  apiClient.post<{ status: string }>(`/api/admin/data-deletion-requests/${id}/reject`);

export const exportMyData = () =>
  apiClient.get<DataExport>("/api/me/data-export");

export const createDeletionRequest = () =>
  apiClient.post<DeletionRequest>("/api/me/data-deletion-request");

// Download helper: triggers browser file save for the data export JSON
export const downloadMyDataExport = async (): Promise<void> => {
  const { data } = await exportMyData();
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `totra-data-export-${new Date().toISOString().slice(0, 10)}.json`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};
```

### 3b — GDPRPage (admin view)

- [ ] **Step 2: Create `dashboard/src/pages/admin/GDPRPage.tsx`**

```tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getDataRetention,
  setDataRetention,
  runRetentionCleanup,
  listDeletionRequests,
  approveDeletionRequest,
  rejectDeletionRequest,
} from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";

export default function GDPRPage() {
  const qc = useQueryClient();

  // --- Data Retention ---
  const { data: retentionData, isLoading: retLoading } = useQuery({
    queryKey: ["data-retention"],
    queryFn: () => getDataRetention().then((r) => r.data),
  });

  const [monthsInput, setMonthsInput] = useState("");
  const retentionMutation = useMutation({
    mutationFn: (months: number) => setDataRetention(months),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["data-retention"] });
      setMonthsInput("");
    },
  });

  const cleanupMutation = useMutation({
    mutationFn: runRetentionCleanup,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deletion-requests"] }),
  });

  // --- Deletion Requests ---
  const { data: deletionData, isLoading: delLoading } = useQuery({
    queryKey: ["deletion-requests"],
    queryFn: () => listDeletionRequests().then((r) => r.data),
  });

  const approveMutation = useMutation({
    mutationFn: (id: string) => approveDeletionRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deletion-requests"] }),
  });

  const rejectMutation = useMutation({
    mutationFn: (id: string) => rejectDeletionRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deletion-requests"] }),
  });

  const requests = deletionData?.requests ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">GDPR & Compliance</h1>

      {/* Data Retention Policy */}
      <Card>
        <CardHeader>
          <CardTitle>Data Retention Policy</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {retLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : (
            <p className="text-sm text-zinc-400">
              Current retention window:{" "}
              <span className="font-semibold text-zinc-100">
                {retentionData?.data_retention_months ?? "—"} months
              </span>
            </p>
          )}
          <div className="flex gap-3 items-end">
            <div className="space-y-1 w-40">
              <Label>New retention (months)</Label>
              <Input
                type="number"
                min="1"
                placeholder="e.g. 24"
                value={monthsInput}
                onChange={(e) => setMonthsInput(e.target.value)}
              />
            </div>
            <Button
              disabled={!monthsInput || parseInt(monthsInput) < 1 || retentionMutation.isPending}
              onClick={() => retentionMutation.mutate(parseInt(monthsInput))}
            >
              {retentionMutation.isPending ? "Saving..." : "Save"}
            </Button>
          </div>

          <div className="pt-2 border-t border-zinc-800">
            <p className="text-xs text-zinc-500 mb-3">
              Manually trigger deletion of all usage records and output events older than the
              current retention window. This cannot be undone.
            </p>
            <Button
              variant="destructive"
              disabled={cleanupMutation.isPending}
              onClick={() => cleanupMutation.mutate()}
            >
              {cleanupMutation.isPending ? "Running..." : "Run Retention Cleanup"}
            </Button>
            {cleanupMutation.isSuccess && (
              <p className="mt-2 text-sm text-green-400">
                Cleanup complete — {(cleanupMutation.data?.data as any)?.deleted_count ?? 0} records deleted.
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Employee Data Deletion Requests */}
      <Card>
        <CardHeader>
          <CardTitle>
            Data Deletion Requests
            {requests.length > 0 && (
              <Badge variant="destructive" className="ml-2">
                {requests.length} pending
              </Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {delLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : requests.length === 0 ? (
            <p className="text-zinc-500 text-sm">No pending data deletion requests.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Employee</th>
                  <th className="text-left py-2 font-medium">Email</th>
                  <th className="text-left py-2 font-medium">Requested</th>
                  <th className="text-left py-2 font-medium">Status</th>
                  <th className="text-right py-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {requests.map((r) => (
                  <tr key={r.id} className="border-b border-zinc-800/50">
                    <td className="py-2 text-zinc-200 font-medium">{r.user_name ?? r.user_id}</td>
                    <td className="py-2 text-zinc-400">{r.user_email ?? "—"}</td>
                    <td className="py-2 text-zinc-400">{r.requested_at.slice(0, 10)}</td>
                    <td className="py-2">
                      <Badge variant="secondary">{r.status}</Badge>
                    </td>
                    <td className="py-2 text-right flex gap-2 justify-end">
                      <Button
                        size="sm"
                        variant="destructive"
                        disabled={approveMutation.isPending}
                        onClick={() => approveMutation.mutate(r.id)}
                      >
                        Approve & Erase
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={rejectMutation.isPending}
                        onClick={() => rejectMutation.mutate(r.id)}
                      >
                        Reject
                      </Button>
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

### 3c — MyUsagePage additions (employee GDPR actions)

- [ ] **Step 3: Modify `dashboard/src/pages/employee/MyUsagePage.tsx`**

Add the GDPR imports to the existing import block. Find:

```typescript
import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, getMyKPI, getMyFuel, getMyIntegrations, getMyQuota, bindIntegration, apiClient, getMyUID, getMyProfile, updateMyProfile, getMyKPISubmetrics, getMyKPIInsights } from "../../api/client";
```

Replace with:

```typescript
import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, getMyKPI, getMyFuel, getMyIntegrations, getMyQuota, bindIntegration, apiClient, getMyUID, getMyProfile, updateMyProfile, getMyKPISubmetrics, getMyKPIInsights, downloadMyDataExport, createDeletionRequest } from "../../api/client";
```

Then, inside the `MyUsagePage` function body, after the existing `bindMutation` declaration and before the `uid` line, add:

```typescript
  const [deletionRequested, setDeletionRequested] = useState(false);
  const deletionMutation = useMutation({
    mutationFn: createDeletionRequest,
    onSuccess: () => setDeletionRequested(true),
  });

  const [exportLoading, setExportLoading] = useState(false);
  const handleExport = async () => {
    setExportLoading(true);
    try {
      await downloadMyDataExport();
    } finally {
      setExportLoading(false);
    }
  };
```

Then, in the JSX, find the `<div className="flex items-center justify-between">` block that contains the header buttons. Add two new buttons alongside the existing "Link Account" and "Request Quota" buttons:

```tsx
          <Button variant="outline" disabled={exportLoading} onClick={handleExport}>
            {exportLoading ? "Exporting..." : "Export My Data"}
          </Button>
          <Button
            variant="outline"
            disabled={deletionMutation.isPending || deletionRequested}
            onClick={() => {
              if (
                window.confirm(
                  "Submit a request to delete all your usage data? An admin will review this request."
                )
              ) {
                deletionMutation.mutate();
              }
            }}
          >
            {deletionRequested
              ? "Deletion Requested"
              : deletionMutation.isPending
              ? "Submitting..."
              : "Request Data Deletion"}
          </Button>
```

The complete updated header div should look like:

```tsx
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">My Usage — {currentMonth}</h1>
        <div className="flex gap-2">
          <Dialog open={bindOpen} onOpenChange={setBindOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Link Account</Button>
            </DialogTrigger>
            {/* ... existing dialog content unchanged ... */}
          </Dialog>
          <Dialog open={quotaOpen} onOpenChange={setQuotaOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Request Quota</Button>
            </DialogTrigger>
            {/* ... existing dialog content unchanged ... */}
          </Dialog>
          <Button variant="outline" disabled={exportLoading} onClick={handleExport}>
            {exportLoading ? "Exporting..." : "Export My Data"}
          </Button>
          <Button
            variant="outline"
            disabled={deletionMutation.isPending || deletionRequested}
            onClick={() => {
              if (
                window.confirm(
                  "Submit a request to delete all your usage data? An admin will review this request."
                )
              ) {
                deletionMutation.mutate();
              }
            }}
          >
            {deletionRequested
              ? "Deletion Requested"
              : deletionMutation.isPending
              ? "Submitting..."
              : "Request Data Deletion"}
          </Button>
        </div>
      </div>
```

### 3d — App.tsx: add GDPR route

- [ ] **Step 4: Modify `dashboard/src/App.tsx`**

Add the import at the top with the other admin page imports:

```typescript
import GDPRPage from "./pages/admin/GDPRPage";
```

Add the route inside the protected `<Route path="/">` block, after the ROI route:

```tsx
            <Route path="admin/gdpr" element={<ProtectedRoute adminOnly><GDPRPage /></ProtectedRoute>} />
```

### 3e — Layout.tsx: add GDPR nav item

- [ ] **Step 5: Modify `dashboard/src/components/Layout.tsx`**

Find `adminNavItems` and add the GDPR entry after `"ROI Reports"`:

```typescript
const adminNavItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Reports", href: "/admin/reports" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "IP Allowlist", href: "/admin/ip-allowlist" },
  { label: "Bot Notifications", href: "/admin/bot-configs" },
  { label: "HR Sync", href: "/admin/hr-sync" },
  { label: "ROI Reports", href: "/admin/roi" },
  { label: "GDPR & Compliance", href: "/admin/gdpr" },
  { label: "My Usage", href: "/me" },
];
```

### 3f — Verify frontend builds

- [ ] **Step 6: Build the dashboard**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run build 2>&1 | tail -20
```

Expected: `built in Xs` with no TypeScript errors.

- [ ] **Step 7: Commit frontend**

```bash
cd /Users/sugac.275/ToTra && \
  git add dashboard/src/api/client.ts \
          dashboard/src/pages/admin/GDPRPage.tsx \
          dashboard/src/pages/employee/MyUsagePage.tsx \
          dashboard/src/App.tsx \
          dashboard/src/components/Layout.tsx
git commit -m "feat(dashboard): add GDPRPage, data export + deletion request buttons on MyUsagePage"
```

---

## Final Verification

- [ ] **Smoke-test all new endpoints against the running stack**

```bash
# Start the admin service (if not already running)
cd /Users/sugac.275/ToTra && docker compose up -d

# Get an admin token
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@demo.com","password":"demo"}' | jq -r '.token')

# 1. Get retention setting
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/admin/data-retention | jq

# 2. Update retention to 12 months
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"data_retention_months":12}' http://localhost:8081/api/admin/data-retention | jq

# 3. Run cleanup
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/admin/data-retention/run | jq

# 4. List deletion requests (empty)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/admin/data-deletion-requests | jq

# 5. Employee: export data
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/me/data-export | jq '.exported_at, (.usage_records | length)'

# 6. Employee: submit deletion request
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/me/data-deletion-request | jq

# 7. Admin: list pending (should now show 1)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/admin/data-deletion-requests | jq '.requests | length'
```

- [ ] **Run full test suite one final time**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... -timeout 60s -count=1
```

Expected: all tests pass, zero failures.
