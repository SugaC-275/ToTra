# Compliance Platform Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add finance + medical industry PII pattern packs, EU AI Act self-assessment checklist, SIEM audit CSV export, and SOC 2 report endpoint.

**Architecture:** Extends Phase 1 — adds 4 new PII regex patterns to the gateway (globally applied, no per-tenant config needed), a DB-backed EU AI Act checklist service in admin, and two export endpoints (CSV violations, SOC 2 JSON). New dashboard page for the checklist; export buttons on the existing CompliancePage.

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5, regexp (gateway), React 19 + TanStack Query v5 + Tailwind v4

---

## Codebase Reference

- `gateway/middleware/pii.go` — `piiPatterns` slice + `NewPIIMiddleware`. Phase 1 has 5 patterns: `china_phone`, `china_id_card`, `credit_card`, `email`, `bank_account`.
- `gateway/middleware/pii_test.go` — tests use `setupPIIApp()` which calls `middleware.NewPIIMiddleware(nil, "test-tenant")` + Fiber POST route. Blocked = 422.
- `admin/api/compliance.go` — `RegisterComplianceRoutes`, `currentYearMonth()` helper (already defined in this package, don't redefine).
- `admin/main.go` — wires services; add `checklistSvc` after `complianceSvc`.
- `admin/services/compliance.go` — `ComplianceService` with `GetViolations`, `GetRiskScores`, `GetMonthlyReport`.
- `dashboard/src/pages/admin/CompliancePage.tsx` — existing page; add export buttons near the month picker at the top.
- JWT Claims: `c.Locals("claims").(*services.Claims)` — fields: `UserID`, `TenantID`, `Role`. Admin-only check: `claims.Role != "admin"` → 403.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `infra/postgres/017_industry_compliance.sql` | Create | `eu_ai_act_checklist` table |
| `docker-compose.yml` | Modify | Mount 017 after 016 |
| `gateway/middleware/pii.go` | Modify | Add 4 industry PII patterns |
| `gateway/middleware/pii_test.go` | Modify | 4 new test cases |
| `admin/services/compliance_checklist.go` | Create | `ChecklistEntry`, `GetChecklist`, `UpdateItem`, `ExportViolationsCSV`, `GetSOC2Report` |
| `admin/services/compliance_checklist_test.go` | Create | Unit tests for pure validation |
| `admin/api/compliance_checklist.go` | Create | `RegisterChecklistRoutes` + 4 handlers |
| `admin/main.go` | Modify | Wire `ChecklistService` |
| `dashboard/src/pages/admin/ComplianceChecklistPage.tsx` | Create | EU AI Act checklist UI |
| `dashboard/src/pages/admin/CompliancePage.tsx` | Modify | Add CSV + SOC2 export buttons |
| `dashboard/src/App.tsx` | Modify | Add `/admin/compliance/checklist` route |
| `dashboard/src/components/Layout.tsx` | Modify | Add "EU AI Act" nav item |

---

## Task 1: Industry PII Patterns (Finance + Medical)

**Files:**
- Modify: `gateway/middleware/pii.go`
- Modify: `gateway/middleware/pii_test.go`

- [ ] **Step 1: Write 4 failing tests**

Append to `gateway/middleware/pii_test.go`:

```go
func TestPIIMiddleware_ContractAmount(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"审核合同金额：12,345,678元的采购合同"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_TransactionID(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"查询交易流水号：TX20260514001234的状态"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_PatientID(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"患者ID: P20260514001的检查报告"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_ICDCode(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"患者诊断：ICD-10-A01.0，请给出治疗方案"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}
```

- [ ] **Step 2: Run tests to confirm they FAIL**

```bash
cd /path/to/worktree/gateway
go test ./middleware/ -run 'TestPIIMiddleware_Contract|TestPIIMiddleware_Transaction|TestPIIMiddleware_Patient|TestPIIMiddleware_ICD' -v
```

Expected: FAIL — patterns don't exist yet.

- [ ] **Step 3: Add 4 patterns to piiPatterns**

In `gateway/middleware/pii.go`, append to the `piiPatterns` slice:

```go
var piiPatterns = []*piiRule{
	{name: "china_phone", re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
	{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{name: "bank_account", re: regexp.MustCompile(`\b\d{16,19}\b`)},
	// Finance industry
	{name: "contract_amount", re: regexp.MustCompile(`(?:合同金额|合同价款)[：:\s]*[¥￥]?\d[\d,\.]*(?:万|千万|亿)?元?`)},
	{name: "transaction_id", re: regexp.MustCompile(`(?:交易流水号|流水号|交易编号)[：:\s]*[A-Za-z0-9\-]{8,}`)},
	// Medical industry
	{name: "patient_id", re: regexp.MustCompile(`(?:患者[Ii][Dd]|患者编号|病历号|住院号)[：:\s]*[A-Za-z0-9\-]{4,20}`)},
	{name: "icd_code", re: regexp.MustCompile(`\bICD[-–]?(?:10|11)[-–]\s*[A-Z]\d{2}(?:\.\d{1,4})?\b`)},
}
```

- [ ] **Step 4: Run tests to confirm all pass**

```bash
cd /path/to/worktree/gateway
go test ./middleware/ -v
```

Expected: all PASS including the 4 new tests.

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/pii.go gateway/middleware/pii_test.go
git commit -m "feat(compliance): industry PII packs — finance contract/transaction, medical patient-ID/ICD-10"
```

---

## Task 2: Migration 017 + EU AI Act Checklist Service

**Files:**
- Create: `infra/postgres/017_industry_compliance.sql`
- Modify: `docker-compose.yml`
- Create: `admin/services/compliance_checklist.go`
- Create: `admin/services/compliance_checklist_test.go`

- [ ] **Step 1: Write 2 failing unit tests**

Create `admin/services/compliance_checklist_test.go`:

```go
package services_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestChecklistService_UpdateItem_InvalidStatus(t *testing.T) {
	svc := services.NewChecklistService(nil)
	err := svc.UpdateItem(context.Background(), "tid", "data_governance", "wrong_status", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestChecklistService_UpdateItem_InvalidKey(t *testing.T) {
	svc := services.NewChecklistService(nil)
	err := svc.UpdateItem(context.Background(), "tid", "nonexistent_key", "compliant", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid checklist item")
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestChecklistService' -v
```

Expected: FAIL — type not defined yet.

- [ ] **Step 3: Create the migration**

Create `infra/postgres/017_industry_compliance.sql`:

```sql
CREATE TABLE IF NOT EXISTS eu_ai_act_checklist (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    item_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'not_assessed',
    notes TEXT NOT NULL DEFAULT '',
    assessed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, item_key)
);

CREATE INDEX IF NOT EXISTS idx_eu_checklist_tenant ON eu_ai_act_checklist(tenant_id);
```

- [ ] **Step 4: Add docker-compose mount**

In `docker-compose.yml`, in the `postgres.volumes` list, add after the `016_drop_kpi_tables.sql` line:

```yaml
      - ./infra/postgres/017_industry_compliance.sql:/docker-entrypoint-initdb.d/017_industry_compliance.sql:ro
```

- [ ] **Step 5: Implement ChecklistService**

Create `admin/services/compliance_checklist.go`:

```go
package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var checklistKeys = []string{
	"data_governance",
	"human_oversight",
	"transparency",
	"accuracy_robustness",
	"record_keeping",
	"risk_management",
}

var checklistLabels = map[string]string{
	"data_governance":     "Data Governance & Management",
	"human_oversight":     "Human Oversight",
	"transparency":        "Transparency Obligations",
	"accuracy_robustness": "Accuracy, Robustness & Cybersecurity",
	"record_keeping":      "Record-Keeping & Logging",
	"risk_management":     "Risk Management System",
}

var checklistDescriptions = map[string]string{
	"data_governance":     "Training data quality standards and governance practices are documented and implemented",
	"human_oversight":     "Mechanisms enabling human oversight, intervention and correction of AI decisions",
	"transparency":        "Users informed they interact with AI; system limitations and capabilities are documented",
	"accuracy_robustness": "System achieves appropriate accuracy and is robust against errors and adversarial inputs",
	"record_keeping":      "Events throughout the AI system lifecycle are automatically logged and retained",
	"risk_management":     "Continuous risk identification, analysis, evaluation and mitigation process is in place",
}

var validStatuses = map[string]bool{
	"compliant":     true,
	"partial":       true,
	"non_compliant": true,
	"not_assessed":  true,
}

var validChecklistKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range checklistKeys {
		m[k] = true
	}
	return m
}()

// ChecklistEntry is a single EU AI Act checklist item with saved state merged in.
type ChecklistEntry struct {
	ItemKey     string  `json:"item_key"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Notes       string  `json:"notes"`
	AssessedAt  *string `json:"assessed_at"`
}

// SOC2Report is a structured summary for a SOC 2 Type II AI appendix.
type SOC2Report struct {
	GeneratedAt           string `json:"generated_at"`
	TenantID              string `json:"tenant_id"`
	AuditLogEntryCount    int64  `json:"audit_log_entry_count"`
	AuditLogLastHash      string `json:"audit_log_last_hash"`
	PIIViolations12Months int    `json:"pii_violations_12_months"`
	IPAllowlistCount      int    `json:"ip_allowlist_count"`
	EUAIActCompliantCount int    `json:"eu_ai_act_compliant_count"`
	EUAIActTotalItems     int    `json:"eu_ai_act_total_items"`
}

// ChecklistService manages EU AI Act checklist and compliance exports.
type ChecklistService struct {
	pool *pgxpool.Pool
}

func NewChecklistService(pool *pgxpool.Pool) *ChecklistService {
	return &ChecklistService{pool: pool}
}

// GetChecklist returns all 6 EU AI Act checklist items with saved state for the tenant.
func (s *ChecklistService) GetChecklist(ctx context.Context, tenantID string) ([]*ChecklistEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT item_key, status, notes, assessed_at
		 FROM eu_ai_act_checklist WHERE tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	saved := map[string]*ChecklistEntry{}
	for rows.Next() {
		e := &ChecklistEntry{}
		var assessedAt *time.Time
		if err := rows.Scan(&e.ItemKey, &e.Status, &e.Notes, &assessedAt); err != nil {
			return nil, err
		}
		if assessedAt != nil {
			s2 := assessedAt.Format(time.RFC3339)
			e.AssessedAt = &s2
		}
		saved[e.ItemKey] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]*ChecklistEntry, 0, len(checklistKeys))
	for _, key := range checklistKeys {
		entry := &ChecklistEntry{
			ItemKey:     key,
			Label:       checklistLabels[key],
			Description: checklistDescriptions[key],
			Status:      "not_assessed",
			Notes:       "",
		}
		if sv, ok := saved[key]; ok {
			entry.Status = sv.Status
			entry.Notes = sv.Notes
			entry.AssessedAt = sv.AssessedAt
		}
		result = append(result, entry)
	}
	return result, nil
}

// UpdateItem saves an assessment for one checklist item (upsert).
func (s *ChecklistService) UpdateItem(ctx context.Context, tenantID, itemKey, status, notes string) error {
	if !validStatuses[status] {
		return fmt.Errorf("invalid status: %q", status)
	}
	if !validChecklistKeys[itemKey] {
		return fmt.Errorf("invalid checklist item: %q", itemKey)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO eu_ai_act_checklist (tenant_id, item_key, status, notes, assessed_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (tenant_id, item_key) DO UPDATE
		 SET status = EXCLUDED.status, notes = EXCLUDED.notes,
		     assessed_at = NOW(), updated_at = NOW()`,
		tenantID, itemKey, status, notes,
	)
	return err
}

// ExportViolationsCSV returns a CSV string of PII violations for the given yearMonth (YYYY-MM).
func (s *ChecklistService) ExportViolationsCSV(ctx context.Context, tenantID, yearMonth string) (string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT v.occurred_at, COALESCE(u.email, 'unknown'), v.pii_type, v.action, COALESCE(v.request_path, '')
		 FROM pii_violations v
		 LEFT JOIN users u ON u.id = v.user_id
		 WHERE v.tenant_id = $1
		   AND to_char(v.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		 ORDER BY v.occurred_at DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var buf strings.Builder
	buf.WriteString("occurred_at,user_email,pii_type,action,request_path\n")
	for rows.Next() {
		var occurredAt time.Time
		var email, piiType, action, path string
		if err := rows.Scan(&occurredAt, &email, &piiType, &action, &path); err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, "%s,%s,%s,%s,%s\n",
			occurredAt.UTC().Format(time.RFC3339),
			csvEscape(email), csvEscape(piiType), csvEscape(action), csvEscape(path),
		)
	}
	return buf.String(), rows.Err()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, `",`) {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// GetSOC2Report assembles a structured SOC 2 Type II AI appendix report.
func (s *ChecklistService) GetSOC2Report(ctx context.Context, tenantID string) (*SOC2Report, error) {
	report := &SOC2Report{
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		TenantID:          tenantID,
		EUAIActTotalItems: len(checklistKeys),
	}

	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(entry_hash), '') FROM audit_log WHERE tenant_id = $1`,
		tenantID,
	).Scan(&report.AuditLogEntryCount, &report.AuditLogLastHash)

	cutoff := time.Now().UTC().AddDate(0, -12, 0)
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pii_violations WHERE tenant_id = $1 AND occurred_at >= $2`,
		tenantID, cutoff,
	).Scan(&report.PIIViolations12Months)

	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ip_allowlist WHERE tenant_id = $1 AND enabled = true`,
		tenantID,
	).Scan(&report.IPAllowlistCount)

	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM eu_ai_act_checklist WHERE tenant_id = $1 AND status = 'compliant'`,
		tenantID,
	).Scan(&report.EUAIActCompliantCount)

	return report, nil
}
```

- [ ] **Step 6: Run tests to confirm PASS**

```bash
cd /path/to/worktree/admin
go test ./services/ -run 'TestChecklistService' -v
```

Expected: both tests PASS (validation happens before DB call, so nil pool is fine).

- [ ] **Step 7: Commit**

```bash
git add infra/postgres/017_industry_compliance.sql docker-compose.yml \
        admin/services/compliance_checklist.go admin/services/compliance_checklist_test.go
git commit -m "feat(compliance): migration 017 + EU AI Act checklist service (GetChecklist, UpdateItem, CSV export, SOC2 report)"
```

---

## Task 3: Checklist API Routes + Wire Admin Main

**Files:**
- Create: `admin/api/compliance_checklist.go`
- Modify: `admin/main.go`

- [ ] **Step 1: Write failing handler tests**

Create `admin/api/compliance_checklist_test.go`:

```go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubChecklistSvc struct{}

func (s *stubChecklistSvc) GetChecklist(_ context.Context, _ string) ([]*services.ChecklistEntry, error) {
	return []*services.ChecklistEntry{
		{ItemKey: "data_governance", Label: "Data Governance & Management", Status: "not_assessed"},
	}, nil
}
func (s *stubChecklistSvc) UpdateItem(_ context.Context, _, _, _, _ string) error { return nil }
func (s *stubChecklistSvc) ExportViolationsCSV(_ context.Context, _, _ string) (string, error) {
	return "occurred_at,user_email,pii_type,action,request_path\n", nil
}
func (s *stubChecklistSvc) GetSOC2Report(_ context.Context, _ string) (*services.SOC2Report, error) {
	return &services.SOC2Report{TenantID: "tid"}, nil
}

func setupChecklistApp(svc api.ChecklistServiceIface) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterChecklistRoutes(app, svc)
	return app
}

func TestGetChecklist_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/checklist", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestUpdateChecklistItem_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	body := `{"status":"compliant","notes":"verified"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compliance/checklist/data_governance",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 204, resp.StatusCode)
}

func TestUpdateChecklistItem_MissingStatus(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compliance/checklist/data_governance",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestExportViolationsCSV_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/audit-export", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "text/csv", resp.Header.Get("Content-Type"))
}

func TestGetSOC2Report_OK(t *testing.T) {
	app := setupChecklistApp(&stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/soc2-report", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestChecklistRoutes_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{Role: "employee"})
		return c.Next()
	})
	api.RegisterChecklistRoutes(app, &stubChecklistSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/compliance/checklist", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/admin
go test ./api/ -run 'TestGetChecklist|TestUpdateChecklist|TestExportViolations|TestGetSOC2|TestChecklistRoutes' -v
```

Expected: compile error — `api.RegisterChecklistRoutes`, `api.ChecklistServiceIface` not defined.

- [ ] **Step 3: Implement the API handlers**

Create `admin/api/compliance_checklist.go`:

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// ChecklistServiceIface allows stub injection in tests.
type ChecklistServiceIface interface {
	GetChecklist(ctx context.Context, tenantID string) ([]*services.ChecklistEntry, error)
	UpdateItem(ctx context.Context, tenantID, itemKey, status, notes string) error
	ExportViolationsCSV(ctx context.Context, tenantID, yearMonth string) (string, error)
	GetSOC2Report(ctx context.Context, tenantID string) (*services.SOC2Report, error)
}

func RegisterChecklistRoutes(app fiber.Router, svc ChecklistServiceIface) {
	app.Get("/api/admin/compliance/checklist", getChecklist(svc))
	app.Put("/api/admin/compliance/checklist/:key", updateChecklistItem(svc))
	app.Get("/api/admin/compliance/audit-export", exportViolationsCSV(svc))
	app.Get("/api/admin/compliance/soc2-report", getSOC2Report(svc))
}

func getChecklist(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		items, err := svc.GetChecklist(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"items": items})
	}
}

func updateChecklistItem(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Status string `json:"status"`
			Notes  string `json:"notes"`
		}
		if err := c.BodyParser(&body); err != nil || body.Status == "" {
			return c.Status(400).JSON(fiber.Map{"error": "status required"})
		}
		if err := svc.UpdateItem(c.Context(), claims.TenantID, c.Params("key"), body.Status, body.Notes); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(204)
	}
}

func exportViolationsCSV(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		month := c.Query("month", currentYearMonth())
		csv, err := svc.ExportViolationsCSV(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", `attachment; filename="compliance-violations-`+month+`.csv"`)
		return c.SendString(csv)
	}
}

func getSOC2Report(svc ChecklistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		report, err := svc.GetSOC2Report(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(report)
	}
}
```

- [ ] **Step 4: Wire into admin/main.go**

In `admin/main.go`, after `complianceSvc := services.NewComplianceService(pool)`, add:

```go
checklistSvc := services.NewChecklistService(pool)
```

And after `api.RegisterComplianceRoutes(protected, complianceSvc)`, add:

```go
api.RegisterChecklistRoutes(protected, checklistSvc)
```

- [ ] **Step 5: Confirm build + tests pass**

```bash
cd /path/to/worktree/admin
go build ./...
go test ./api/ -run 'TestGetChecklist|TestUpdateChecklist|TestExportViolations|TestGetSOC2|TestChecklistRoutes' -v
```

Expected: build succeeds, all 6 new tests PASS.

- [ ] **Step 6: Commit**

```bash
git add admin/api/compliance_checklist.go admin/api/compliance_checklist_test.go admin/main.go
git commit -m "feat(compliance): checklist API — GET checklist, PUT item, CSV export, SOC2 report"
```

---

## Task 4: Dashboard — Export Buttons + EU AI Act Checklist Page

**Files:**
- Modify: `dashboard/src/pages/admin/CompliancePage.tsx`
- Create: `dashboard/src/pages/admin/ComplianceChecklistPage.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

- [ ] **Step 1: Add export buttons to CompliancePage.tsx**

Read the current `dashboard/src/pages/admin/CompliancePage.tsx` to find where the month picker `<select>` or heading is. Add two buttons immediately after the month picker:

```tsx
{/* Add these two buttons next to/after the month picker */}
<a
  href={`/api/admin/compliance/audit-export?month=${month}`}
  download
  className="px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded-lg font-medium"
>
  Export CSV
</a>
<button
  onClick={() =>
    apiClient
      .get("/api/admin/compliance/soc2-report")
      .then((r) => {
        const blob = new Blob([JSON.stringify(r.data, null, 2)], {
          type: "application/json",
        });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `soc2-report-${month}.json`;
        a.click();
        URL.revokeObjectURL(url);
      })
  }
  className="px-3 py-1.5 text-sm bg-blue-100 hover:bg-blue-200 text-blue-700 rounded-lg font-medium"
>
  SOC 2 Report
</button>
```

You must import `apiClient` from `../../api/client` if not already imported.

The `month` variable must already be a `string` state value in the component — read the file to confirm. If it doesn't exist, check what variable holds the current year-month and use it.

- [ ] **Step 2: Create ComplianceChecklistPage.tsx**

Create `dashboard/src/pages/admin/ComplianceChecklistPage.tsx`:

```tsx
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface ChecklistEntry {
  item_key: string;
  label: string;
  description: string;
  status: "not_assessed" | "compliant" | "partial" | "non_compliant";
  notes: string;
  assessed_at: string | null;
}

const STATUS_COLORS: Record<string, string> = {
  compliant: "text-green-700 bg-green-50 border-green-200",
  partial: "text-yellow-700 bg-yellow-50 border-yellow-200",
  non_compliant: "text-red-700 bg-red-50 border-red-200",
  not_assessed: "text-gray-500 bg-gray-50 border-gray-200",
};

const STATUS_LABELS: Record<string, string> = {
  compliant: "Compliant",
  partial: "Partial",
  non_compliant: "Non-Compliant",
  not_assessed: "Not Assessed",
};

export default function ComplianceChecklistPage() {
  const qc = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ["compliance-checklist"],
    queryFn: () =>
      apiClient
        .get<{ items: ChecklistEntry[] }>("/api/admin/compliance/checklist")
        .then((r) => r.data.items),
  });

  const updateMutation = useMutation({
    mutationFn: ({
      key,
      status,
      notes,
    }: {
      key: string;
      status: string;
      notes: string;
    }) =>
      apiClient.put(`/api/admin/compliance/checklist/${key}`, {
        status,
        notes,
      }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["compliance-checklist"] }),
  });

  if (isLoading) {
    return <div className="p-8 text-gray-400">Loading checklist…</div>;
  }

  const items = data ?? [];
  const compliantCount = items.filter((i) => i.status === "compliant").length;

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-1">EU AI Act Compliance Checklist</h1>
      <p className="text-gray-500 text-sm mb-6">
        Self-assessment for high-risk AI system requirements. Select a status for
        each item to track your compliance posture.
      </p>

      <div className="mb-6 px-4 py-3 bg-blue-50 border border-blue-100 rounded-lg text-sm text-blue-700">
        <span className="font-semibold">{compliantCount}</span> /{" "}
        {items.length} items marked compliant
      </div>

      <div className="space-y-3">
        {items.map((item) => (
          <div
            key={item.item_key}
            className={`border rounded-lg p-4 ${STATUS_COLORS[item.status]}`}
          >
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <h3 className="font-semibold text-gray-800">{item.label}</h3>
                  <span className="text-xs px-2 py-0.5 rounded-full border font-medium">
                    {STATUS_LABELS[item.status]}
                  </span>
                </div>
                <p className="text-sm text-gray-600">{item.description}</p>
                {item.assessed_at && (
                  <p className="text-xs text-gray-400 mt-1">
                    Last assessed:{" "}
                    {new Date(item.assessed_at).toLocaleDateString()}
                  </p>
                )}
              </div>
              <select
                value={item.status}
                onChange={(e) =>
                  updateMutation.mutate({
                    key: item.item_key,
                    status: e.target.value,
                    notes: item.notes,
                  })
                }
                className="text-sm border border-gray-200 rounded px-2 py-1 bg-white text-gray-700 min-w-[140px]"
              >
                <option value="not_assessed">Not Assessed</option>
                <option value="compliant">Compliant</option>
                <option value="partial">Partial</option>
                <option value="non_compliant">Non-Compliant</option>
              </select>
            </div>
            {item.notes && (
              <p className="mt-2 text-sm text-gray-600 border-t border-gray-200 pt-2">
                {item.notes}
              </p>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Add route to App.tsx**

In `dashboard/src/App.tsx`, add the import after the existing CompliancePage import:

```tsx
import ComplianceChecklistPage from "./pages/admin/ComplianceChecklistPage";
```

In the `<Routes>` block, after the `admin/compliance` route:

```tsx
<Route path="admin/compliance/checklist" element={<ProtectedRoute adminOnly><ComplianceChecklistPage /></ProtectedRoute>} />
```

- [ ] **Step 4: Add nav item to Layout.tsx**

In `dashboard/src/components/Layout.tsx`, in the `adminNavItems` array, after the `"Compliance Center"` entry:

```tsx
{ label: "EU AI Act", href: "/admin/compliance/checklist" },
```

- [ ] **Step 5: TypeScript check**

```bash
cd /path/to/worktree/dashboard
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/admin/CompliancePage.tsx \
        dashboard/src/pages/admin/ComplianceChecklistPage.tsx \
        dashboard/src/App.tsx \
        dashboard/src/components/Layout.tsx
git commit -m "feat(compliance): EU AI Act checklist page + CSV/SOC2 export buttons on CompliancePage"
```

---

## Self-Review

**Spec coverage:**
- ✅ 金融行业 PII (contract amount, transaction ID) — Task 1
- ✅ 医疗行业 PHI (patient ID, ICD-10) — Task 1
- ✅ SOC 2 Type II AI 附录报告 — Task 2 (`GetSOC2Report`), Task 3 (API endpoint), Task 4 (download button)
- ✅ EU AI Act 合规自检清单 — Task 2 (DB + service), Task 3 (API), Task 4 (ChecklistPage)
- ✅ 合规审计 API for SIEM — Task 2 (`ExportViolationsCSV`), Task 3 (CSV download endpoint with proper `Content-Disposition`)

**Placeholder scan:** none found.

**Type consistency:** `ChecklistEntry` defined in `compliance_checklist.go`, used in `ChecklistServiceIface` and `ComplianceChecklistPage`. `SOC2Report` defined in `compliance_checklist.go`, returned by service + API. No mismatches.
