# AI Compliance Platform — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将现有 PII 检测和审计能力包装成完整合规产品：持久化 PII 违规事件、风险评分、实时 Bot 告警、合规中心 Dashboard。

**Architecture:** Gateway 的 PII 中间件检测到违规后，异步写入 `pii_violations` 表（与 AgentStore 的异步 channel 模式一致）。Admin service 新增 ComplianceService 读取违规记录、计算用户/部门风险评分、生成月度报告。Bot 通知在违规发生时实时推送。Dashboard 新增 CompliancePage（仅 admin 可见）。

**Tech Stack:** Go 1.26 + Fiber v2 + pgx/v5（gateway 直接写 PG，与 agent_store 同模式），React 19 + TanStack Query v5 + Tailwind v4

**Spec:** `docs/superpowers/plans/2026-05-14-product-strategy.md`

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `infra/postgres/015_pii_violations.sql` | Create | `pii_violations` 表 |
| `docker-compose.yml` | Modify | 加入 migration 015 volume |
| `gateway/storage/pii_store.go` | Create | 异步写入 pii_violations（channel + goroutine） |
| `gateway/storage/pii_store_test.go` | Create | 测试 BuildViolationRecord |
| `gateway/middleware/pii.go` | Modify | 检测到 PII 时写入 store，增加更多 PII 类型 |
| `gateway/middleware/pii_test.go` | Modify | 覆盖新 PII 类型 |
| `admin/services/compliance.go` | Create | ComplianceService：风险评分、违规查询、月报 |
| `admin/services/compliance_test.go` | Create | 测试 ComputeRiskScore（纯函数） |
| `admin/api/compliance.go` | Create | REST 路由：violations / risk-scores / report / alert |
| `admin/api/compliance_test.go` | Create | HTTP handler 测试（mockService） |
| `dashboard/src/pages/admin/CompliancePage.tsx` | Create | 合规中心：违规事件表 + 风险评分卡 + 实时告警开关 |

---

## Task 1: Migration 015 + docker-compose

**Files:**
- Create: `infra/postgres/015_pii_violations.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: 创建 migration 文件**

```sql
-- infra/postgres/015_pii_violations.sql
CREATE TABLE pii_violations (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   UUID         NOT NULL,
    user_id     UUID,                          -- NULL if user unknown at gateway
    pii_type    VARCHAR(50)  NOT NULL,          -- china_phone | china_id_card | credit_card | email | bank_account
    action      VARCHAR(20)  NOT NULL DEFAULT 'blocked',  -- blocked | warned
    request_path VARCHAR(200),
    occurred_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX ON pii_violations(tenant_id, occurred_at DESC);
CREATE INDEX ON pii_violations(tenant_id, user_id, occurred_at DESC);
```

- [ ] **Step 2: 在 docker-compose.yml postgres volumes 末尾添加**

找到 postgres service 的 volumes 块，在 014 行之后加一行：

```yaml
      - ./infra/postgres/015_pii_violations.sql:/docker-entrypoint-initdb.d/015_pii_violations.sql:ro
```

- [ ] **Step 3: Commit**

```bash
git add infra/postgres/015_pii_violations.sql docker-compose.yml
git commit -m "feat(compliance): migration 015 — pii_violations table"
```

---

## Task 2: PiiStore — 异步违规记录

**Files:**
- Create: `gateway/storage/pii_store.go`
- Create: `gateway/storage/pii_store_test.go`

- [ ] **Step 1: 写失败的测试**

创建 `gateway/storage/pii_store_test.go`：

```go
package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/storage"
)

func TestBuildViolationRecord_AllFields(t *testing.T) {
	r := storage.BuildViolationRecord("tenant-1", "user-1", "china_phone", "blocked", "/v1/chat/completions")
	assert.Equal(t, "tenant-1", r.TenantID)
	assert.Equal(t, "user-1", r.UserID)
	assert.Equal(t, "china_phone", r.PIIType)
	assert.Equal(t, "blocked", r.Action)
	assert.Equal(t, "/v1/chat/completions", r.RequestPath)
}

func TestBuildViolationRecord_EmptyUser(t *testing.T) {
	r := storage.BuildViolationRecord("tenant-1", "", "credit_card", "blocked", "/v1/messages")
	assert.Equal(t, "", r.UserID)
	assert.Equal(t, "credit_card", r.PIIType)
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run TestBuildViolation -v 2>&1 | head -15
```

Expected: FAIL — `storage.BuildViolationRecord undefined`

- [ ] **Step 3: 实现 pii_store.go**

创建 `gateway/storage/pii_store.go`：

```go
package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ViolationRecord struct {
	TenantID    string
	UserID      string
	PIIType     string
	Action      string
	RequestPath string
}

// BuildViolationRecord is the pure constructor — exported for unit testing.
func BuildViolationRecord(tenantID, userID, piiType, action, requestPath string) ViolationRecord {
	return ViolationRecord{
		TenantID:    tenantID,
		UserID:      userID,
		PIIType:     piiType,
		Action:      action,
		RequestPath: requestPath,
	}
}

// PIIStore writes violation records to Postgres asynchronously via a buffered channel.
// Pattern mirrors AgentStore in gateway/storage/agent_store.go.
type PIIStore struct {
	pool chan ViolationRecord
	db   *pgxpool.Pool
}

func NewPIIStore(db *pgxpool.Pool, bufSize int) *PIIStore {
	s := &PIIStore{
		pool: make(chan ViolationRecord, bufSize),
		db:   db,
	}
	go s.flush()
	return s
}

// Record enqueues a violation; non-blocking (drops if buffer full).
func (s *PIIStore) Record(r ViolationRecord) {
	select {
	case s.pool <- r:
	default:
		log.Println("pii_store: buffer full, dropping violation record")
	}
}

func (s *PIIStore) flush() {
	for r := range s.pool {
		userID := &r.UserID
		if r.UserID == "" {
			userID = nil
		}
		_, err := s.db.Exec(context.Background(),
			`INSERT INTO pii_violations (tenant_id, user_id, pii_type, action, request_path)
			 VALUES ($1, $2, $3, $4, $5)`,
			r.TenantID, userID, r.PIIType, r.Action, r.RequestPath,
		)
		if err != nil {
			log.Printf("pii_store: insert error: %v", err)
		}
	}
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./storage/... -run TestBuildViolation -v 2>&1
```

Expected:
```
--- PASS: TestBuildViolationRecord_AllFields
--- PASS: TestBuildViolationRecord_EmptyUser
PASS
```

- [ ] **Step 5: Commit**

```bash
git add gateway/storage/pii_store.go gateway/storage/pii_store_test.go
git commit -m "feat(compliance): PIIStore — async violation recording to pii_violations"
```

---

## Task 3: 升级 PII 中间件 — 持久化 + 更多模式

**Files:**
- Modify: `gateway/middleware/pii.go`
- Modify: `gateway/middleware/pii_test.go`

当前 pii.go 只检测 3 种模式且不持久化。新版增加邮箱、银行卡号，并接收 PIIStore 写入违规记录。

- [ ] **Step 1: 在 pii_test.go 末尾新增测试**

```go
func TestPIIMiddleware_EmailBlocked(t *testing.T) {
	app := fiber.New()
	// nil store = no-op persistence (ok for unit test)
	app.Use(middleware.NewPIIMiddleware(nil, "tenant-1"))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	body := `{"messages":[{"content":"联系我 foo@example.com 获取报价"}]}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_CleanRequestPasses(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "tenant-1"))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	body := `{"messages":[{"content":"帮我写一个排序算法"}]}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Step 2: 运行新测试，确认失败**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./middleware/... -run TestPIIMiddleware -v 2>&1 | head -20
```

Expected: FAIL — `NewPIIMiddleware` signature mismatch

- [ ] **Step 3: 重写 pii.go**

```go
package middleware

import (
	"regexp"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/storage"
)

var piiPatterns = []*piiRule{
	{name: "china_phone",    re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{name: "china_id_card",  re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "credit_card",    re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
	{name: "email",          re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{name: "bank_account",   re: regexp.MustCompile(`\b\d{16,19}\b`)},
}

type piiRule struct {
	name string
	re   *regexp.Regexp
}

// NewPIIMiddleware detects PII in request bodies. If store is non-nil, violations
// are recorded asynchronously. tenantID is used for violation records.
func NewPIIMiddleware(store *storage.PIIStore, tenantID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
				if store != nil {
					userID := c.Locals("userID")
					uid := ""
					if userID != nil {
						uid, _ = userID.(string)
					}
					store.Record(storage.BuildViolationRecord(
						tenantID, uid, rule.name, "blocked", c.Path(),
					))
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

- [ ] **Step 4: 在 gateway/main.go 中更新 PII 中间件初始化**

找到 `NewPIIMiddleware()` 的调用处，将其改为：

```go
piiStore := storage.NewPIIStore(pool, 1000)
app.Use(middleware.NewPIIMiddleware(piiStore, os.Getenv("TENANT_ID")))
```

> 注：如果 gateway 是多租户的（tenant_id 从 JWT 提取），则在中间件内从 `c.Locals("tenantID")` 读取，而不是从 env 读。确认 gateway main.go 中 tenant 的注入方式后调整。

- [ ] **Step 5: 运行全部 gateway 测试**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./... 2>&1
```

Expected: `ok` for all packages.

- [ ] **Step 6: Commit**

```bash
git add gateway/middleware/pii.go gateway/middleware/pii_test.go
git commit -m "feat(compliance): PII middleware — persist violations + add email/bank_account patterns"
```

---

## Task 4: ComplianceService — 风险评分与报告

**Files:**
- Create: `admin/services/compliance.go`
- Create: `admin/services/compliance_test.go`

- [ ] **Step 1: 写失败的测试（纯函数部分）**

创建 `admin/services/compliance_test.go`：

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestComputeRiskScore_NoViolations(t *testing.T) {
	score := services.ComputeRiskScore(0, 0)
	assert.Equal(t, 0, score)
}

func TestComputeRiskScore_LowRisk(t *testing.T) {
	// 1 violation, 1000 requests → very low risk
	score := services.ComputeRiskScore(1, 1000)
	assert.True(t, score < 20, "1/1000 should be low risk, got %d", score)
}

func TestComputeRiskScore_HighRisk(t *testing.T) {
	// 50 violations, 100 requests → high risk
	score := services.ComputeRiskScore(50, 100)
	assert.True(t, score >= 80, "50/100 should be high risk, got %d", score)
}

func TestComputeRiskScore_CapsAt100(t *testing.T) {
	score := services.ComputeRiskScore(9999, 1)
	assert.Equal(t, 100, score)
}

func TestRiskLevel_Labels(t *testing.T) {
	assert.Equal(t, "low",      services.RiskLevel(0))
	assert.Equal(t, "low",      services.RiskLevel(29))
	assert.Equal(t, "medium",   services.RiskLevel(30))
	assert.Equal(t, "medium",   services.RiskLevel(69))
	assert.Equal(t, "high",     services.RiskLevel(70))
	assert.Equal(t, "critical", services.RiskLevel(90))
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestComputeRisk -v 2>&1 | head -10
```

Expected: FAIL — `services.ComputeRiskScore undefined`

- [ ] **Step 3: 实现 compliance.go**

创建 `admin/services/compliance.go`：

```go
package services

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ViolationRecord is one row from pii_violations joined with user info.
type ViolationRecord struct {
	ID          int64  `json:"id"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	UserEmail   string `json:"user_email"`
	Department  string `json:"department"`
	PIIType     string `json:"pii_type"`
	Action      string `json:"action"`
	RequestPath string `json:"request_path"`
	OccurredAt  string `json:"occurred_at"`
}

// UserRiskScore aggregates violations per user into a risk score.
type UserRiskScore struct {
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	UserEmail      string `json:"user_email"`
	Department     string `json:"department"`
	ViolationCount int    `json:"violation_count"`
	RequestCount   int64  `json:"request_count"`
	RiskScore      int    `json:"risk_score"`   // 0–100
	RiskLevel      string `json:"risk_level"`   // low | medium | high | critical
}

// ComplianceReport is the monthly summary sent to compliance officers.
type ComplianceReport struct {
	YearMonth        string          `json:"year_month"`
	TotalViolations  int             `json:"total_violations"`
	UniqueUsers      int             `json:"unique_users"`
	TopPIITypes      []PIITypeStat   `json:"top_pii_types"`
	HighRiskUsers    []UserRiskScore `json:"high_risk_users"`
}

type PIITypeStat struct {
	PIIType string `json:"pii_type"`
	Count   int    `json:"count"`
}

// ComputeRiskScore returns 0–100 based on violations vs total requests.
// Exported for unit testing.
func ComputeRiskScore(violations int, totalRequests int64) int {
	if violations == 0 || totalRequests == 0 {
		return 0
	}
	ratio := float64(violations) / float64(totalRequests)
	// Logarithmic scale: ratio of 0.5 → 100, 0.001 → ~20
	raw := 100 * math.Log1p(ratio*10) / math.Log1p(10)
	if raw > 100 {
		return 100
	}
	return int(raw)
}

// RiskLevel converts a numeric score to a label.
func RiskLevel(score int) string {
	switch {
	case score >= 90:
		return "critical"
	case score >= 70:
		return "high"
	case score >= 30:
		return "medium"
	default:
		return "low"
	}
}

type ComplianceService struct {
	pool *pgxpool.Pool
}

func NewComplianceService(pool *pgxpool.Pool) *ComplianceService {
	return &ComplianceService{pool: pool}
}

// GetViolations returns recent PII violations for a tenant, newest first.
func (s *ComplianceService) GetViolations(ctx context.Context, tenantID, yearMonth string, limit int) ([]*ViolationRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, COALESCE(v.user_id::text,''), COALESCE(u.name,'unknown'),
		       COALESCE(u.email,''), COALESCE(u.department,''),
		       v.pii_type, v.action, COALESCE(v.request_path,''),
		       to_char(v.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS')
		FROM pii_violations v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.tenant_id = $1
		  AND to_char(v.occurred_at,'YYYY-MM') = $2
		ORDER BY v.occurred_at DESC
		LIMIT $3`, tenantID, yearMonth, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ViolationRecord
	for rows.Next() {
		var r ViolationRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.UserName, &r.UserEmail,
			&r.Department, &r.PIIType, &r.Action, &r.RequestPath, &r.OccurredAt); err != nil {
			return nil, err
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// GetRiskScores returns per-user risk scores for a tenant this month.
func (s *ComplianceService) GetRiskScores(ctx context.Context, tenantID, yearMonth string) ([]*UserRiskScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.user_id, COALESCE(u.name,'unknown'), COALESCE(u.email,''),
		       COALESCE(u.department,''), COUNT(*) AS violation_count,
		       COALESCE((SELECT COUNT(*) FROM usage_records r2
		                 WHERE r2.user_id = v.user_id
		                   AND to_char(r2.created_at,'YYYY-MM') = $2), 0) AS request_count
		FROM pii_violations v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.tenant_id = $1
		  AND to_char(v.occurred_at,'YYYY-MM') = $2
		  AND v.user_id IS NOT NULL
		GROUP BY v.user_id, u.name, u.email, u.department
		ORDER BY violation_count DESC`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*UserRiskScore
	for rows.Next() {
		var r UserRiskScore
		if err := rows.Scan(&r.UserID, &r.UserName, &r.UserEmail,
			&r.Department, &r.ViolationCount, &r.RequestCount); err != nil {
			return nil, err
		}
		r.RiskScore = ComputeRiskScore(r.ViolationCount, r.RequestCount)
		r.RiskLevel = RiskLevel(r.RiskScore)
		result = append(result, &r)
	}
	return result, rows.Err()
}

// GetMonthlyReport generates the compliance summary for a month.
func (s *ComplianceService) GetMonthlyReport(ctx context.Context, tenantID, yearMonth string) (*ComplianceReport, error) {
	report := &ComplianceReport{YearMonth: yearMonth}

	// Total violations + unique users
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT user_id)
		FROM pii_violations
		WHERE tenant_id=$1 AND to_char(occurred_at,'YYYY-MM')=$2`,
		tenantID, yearMonth).Scan(&report.TotalViolations, &report.UniqueUsers)
	if err != nil {
		return nil, err
	}

	// Top PII types
	rows, err := s.pool.Query(ctx, `
		SELECT pii_type, COUNT(*) AS cnt
		FROM pii_violations
		WHERE tenant_id=$1 AND to_char(occurred_at,'YYYY-MM')=$2
		GROUP BY pii_type ORDER BY cnt DESC LIMIT 5`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stat PIITypeStat
		if err := rows.Scan(&stat.PIIType, &stat.Count); err != nil {
			return nil, err
		}
		report.TopPIITypes = append(report.TopPIITypes, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// High-risk users (score >= 70)
	allScores, err := s.GetRiskScores(ctx, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	for _, u := range allScores {
		if u.RiskScore >= 70 {
			report.HighRiskUsers = append(report.HighRiskUsers, *u)
		}
	}

	return report, nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestComputeRisk -v 2>&1
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestRiskLevel -v 2>&1
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add admin/services/compliance.go admin/services/compliance_test.go
git commit -m "feat(compliance): ComplianceService — risk scoring, violations query, monthly report"
```

---

## Task 5: API 路由

**Files:**
- Create: `admin/api/compliance.go`
- Modify: `admin/api/middleware.go` 或 main 路由注册文件（确认路由注册位置）

- [ ] **Step 1: 创建 compliance.go**

```go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ComplianceHandler struct {
	svc *services.ComplianceService
}

func NewComplianceHandler(svc *services.ComplianceService) *ComplianceHandler {
	return &ComplianceHandler{svc: svc}
}

func (h *ComplianceHandler) RegisterRoutes(app fiber.Router, jwtMW fiber.Handler) {
	g := app.Group("/api/admin/compliance", jwtMW, AdminOnly)
	g.Get("/violations", h.getViolations)
	g.Get("/risk-scores", h.getRiskScores)
	g.Get("/report", h.getReport)
}

// GET /api/admin/compliance/violations?month=2026-05&limit=100
func (h *ComplianceHandler) getViolations(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	limit := c.QueryInt("limit", 100)
	if limit > 500 {
		limit = 500
	}
	data, err := h.svc.GetViolations(c.Context(), tenantID, month, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

// GET /api/admin/compliance/risk-scores?month=2026-05
func (h *ComplianceHandler) getRiskScores(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	data, err := h.svc.GetRiskScores(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

// GET /api/admin/compliance/report?month=2026-05
func (h *ComplianceHandler) getReport(c *fiber.Ctx) error {
	tenantID := c.Locals("tenantID").(string)
	month := c.Query("month", currentYearMonth())
	data, err := h.svc.GetMonthlyReport(c.Context(), tenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}
```

> `currentYearMonth()` 是已有的 helper（见 `admin/api/kpi.go` 或类似文件），如果不存在则加：
> ```go
> func currentYearMonth() string {
>     return time.Now().UTC().Format("2006-01")
> }
> ```

- [ ] **Step 2: 在路由注册文件中加入 ComplianceHandler**

找到 `admin/api/middleware.go` 或 `admin/main.go` 中路由注册处，加入：

```go
complianceSvc := services.NewComplianceService(pool)
complianceHandler := api.NewComplianceHandler(complianceSvc)
complianceHandler.RegisterRoutes(app, jwtMiddleware)
```

- [ ] **Step 3: 运行全部 admin 测试**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... 2>&1
```

Expected: `ok` for all packages, 0 failures.

- [ ] **Step 4: Commit**

```bash
git add admin/api/compliance.go
git commit -m "feat(compliance): compliance API routes — violations, risk-scores, report"
```

---

## Task 6: Bot 实时告警

**Files:**
- Modify: `gateway/middleware/pii.go`（已有 PIIStore，加 HTTP 调用 admin bot alert）

Bot 告警通过现有 BotService 推送。Gateway 检测到 PII 后异步 POST 到 admin 的内部端点，admin 调用 BotService 发送消息。

- [ ] **Step 1: 在 admin/api/compliance.go 新增内部告警端点**

在 `RegisterRoutes` 内添加（不加 JWT，只加 internal secret 校验）：

```go
// POST /internal/compliance/pii-alert（gateway 调用，不对外暴露）
app.Post("/internal/compliance/pii-alert", h.handlePIIAlert)
```

实现：

```go
type piiAlertPayload struct {
	TenantID    string `json:"tenant_id"`
	UserID      string `json:"user_id"`
	PIIType     string `json:"pii_type"`
	RequestPath string `json:"request_path"`
}

func (h *ComplianceHandler) handlePIIAlert(c *fiber.Ctx) error {
	secret := c.Get("X-Internal-Secret")
	if secret != os.Getenv("INTERNAL_SECRET") {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	var payload piiAlertPayload
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "bad request"})
	}
	msg := fmt.Sprintf("⚠️ PII 违规告警\n类型: %s\n用户: %s\n路径: %s",
		payload.PIIType, payload.UserID, payload.RequestPath)
	// BotService 广播给该 tenant 的所有 bot
	_ = h.botSvc.BroadcastAlert(c.Context(), payload.TenantID, msg)
	return c.SendStatus(204)
}
```

> `botSvc` 需加入 `ComplianceHandler` struct。在注册路由时注入 `services.NewBotService(pool, encKey)`。

- [ ] **Step 2: 在 PIIStore.flush() 中加 HTTP 回调**

在 `gateway/storage/pii_store.go` 的 `flush()` 内，DB 写成功后：

```go
if adminURL := os.Getenv("ADMIN_INTERNAL_URL"); adminURL != "" {
    go func(rec ViolationRecord) {
        body, _ := json.Marshal(map[string]string{
            "tenant_id": rec.TenantID, "user_id": rec.UserID,
            "pii_type": rec.PIIType, "request_path": rec.RequestPath,
        })
        req, _ := http.NewRequest("POST", adminURL+"/internal/compliance/pii-alert", bytes.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))
        http.DefaultClient.Do(req)
    }(r)
}
```

- [ ] **Step 3: 运行全部测试**

```bash
cd /Users/sugac.275/ToTra/gateway && go test ./... 2>&1
cd /Users/sugac.275/ToTra/admin && go test ./... 2>&1
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add gateway/storage/pii_store.go admin/api/compliance.go
git commit -m "feat(compliance): real-time PII bot alert via internal endpoint"
```

---

## Task 7: Dashboard CompliancePage

**Files:**
- Create: `dashboard/src/pages/admin/CompliancePage.tsx`
- Modify: `dashboard/src/App.tsx`（或路由文件）加路由
- Modify: 侧边栏导航加入 Compliance 入口

- [ ] **Step 1: 创建 CompliancePage.tsx**

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

const API = '/api/admin/compliance'

interface Violation {
  id: number
  user_name: string
  user_email: string
  department: string
  pii_type: string
  action: string
  occurred_at: string
}

interface RiskScore {
  user_name: string
  user_email: string
  department: string
  violation_count: number
  risk_score: number
  risk_level: 'low' | 'medium' | 'high' | 'critical'
}

interface ComplianceReport {
  year_month: string
  total_violations: number
  unique_users: number
  top_pii_types: { pii_type: string; count: number }[]
  high_risk_users: RiskScore[]
}

const riskColors: Record<string, string> = {
  low: 'bg-green-100 text-green-800',
  medium: 'bg-yellow-100 text-yellow-800',
  high: 'bg-orange-100 text-orange-800',
  critical: 'bg-red-100 text-red-800',
}

export default function CompliancePage() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))

  const { data: report } = useQuery<ComplianceReport>({
    queryKey: ['compliance-report', month],
    queryFn: () => fetch(`${API}/report?month=${month}`).then(r => r.json()),
  })

  const { data: violations = [] } = useQuery<Violation[]>({
    queryKey: ['compliance-violations', month],
    queryFn: () => fetch(`${API}/violations?month=${month}&limit=100`).then(r => r.json()),
  })

  const { data: riskScores = [] } = useQuery<RiskScore[]>({
    queryKey: ['compliance-risk', month],
    queryFn: () => fetch(`${API}/risk-scores?month=${month}`).then(r => r.json()),
  })

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">合规中心</h1>
        <input
          type="month"
          value={month}
          onChange={e => setMonth(e.target.value)}
          className="border rounded px-3 py-1 text-sm"
        />
      </div>

      {/* 统计卡 */}
      <div className="grid grid-cols-3 gap-4">
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">本月违规次数</p>
          <p className="text-3xl font-bold text-red-600">{report?.total_violations ?? 0}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">涉及员工数</p>
          <p className="text-3xl font-bold text-orange-500">{report?.unique_users ?? 0}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">高风险用户</p>
          <p className="text-3xl font-bold text-purple-600">{report?.high_risk_users?.length ?? 0}</p>
        </div>
      </div>

      {/* 风险评分表 */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">员工风险评分</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['姓名', '邮箱', '部门', '违规次数', '风险分', '风险等级'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {riskScores.map((u, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2">{u.user_name}</td>
                <td className="px-3 py-2 text-gray-500">{u.user_email}</td>
                <td className="px-3 py-2">{u.department}</td>
                <td className="px-3 py-2">{u.violation_count}</td>
                <td className="px-3 py-2 font-mono">{u.risk_score}</td>
                <td className="px-3 py-2">
                  <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${riskColors[u.risk_level]}`}>
                    {u.risk_level}
                  </span>
                </td>
              </tr>
            ))}
            {riskScores.length === 0 && (
              <tr><td colSpan={6} className="px-3 py-4 text-center text-gray-400">本月无违规记录</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 违规事件明细 */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">违规事件明细</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['时间', '姓名', '部门', 'PII 类型', '处理方式'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {violations.map(v => (
              <tr key={v.id} className="border-t">
                <td className="px-3 py-2 text-gray-500 font-mono text-xs">{v.occurred_at}</td>
                <td className="px-3 py-2">{v.user_name}</td>
                <td className="px-3 py-2">{v.department}</td>
                <td className="px-3 py-2">
                  <span className="bg-red-50 text-red-700 px-2 py-0.5 rounded text-xs">{v.pii_type}</span>
                </td>
                <td className="px-3 py-2">{v.action}</td>
              </tr>
            ))}
            {violations.length === 0 && (
              <tr><td colSpan={5} className="px-3 py-4 text-center text-gray-400">本月无违规记录</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: 在路由文件中注册页面**

在 `dashboard/src/App.tsx`（或路由配置文件）中，找到 admin 路由组，加入：

```tsx
import CompliancePage from './pages/admin/CompliancePage'
// ...
<Route path="/compliance" element={<CompliancePage />} />
```

- [ ] **Step 3: 在侧边栏加入 Compliance 入口**

找到侧边栏组件（通常是 `Sidebar.tsx` 或 `Layout.tsx`），在 admin 菜单项中加入：

```tsx
{ path: '/compliance', label: '合规中心', icon: ShieldCheckIcon }
```

- [ ] **Step 4: 启动前端验证页面可正常渲染**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run dev
```

打开 http://localhost:3000，以 admin 身份登录，确认侧边栏出现"合规中心"，页面无报错。

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/admin/CompliancePage.tsx dashboard/src/App.tsx
git commit -m "feat(compliance): CompliancePage — risk scores, violation log, monthly summary"
```

---

## 验证清单

全部任务完成后：

```bash
# 1. 全量 Go 测试
cd /Users/sugac.275/ToTra/admin && go test ./... -count=1
cd /Users/sugac.275/ToTra/gateway && go test ./... -count=1

# 2. 确认新表存在（需要重建 DB）
docker compose down -v && docker compose up postgres -d
docker compose exec postgres psql -U totra -d totra -c "\dt pii_violations"

# 3. 手动验证 PII 拦截并记录
curl -X POST http://localhost:8082/v1/chat/completions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"我的手机号是13812345678"}]}'
# Expected: 422 pii_blocked

# 4. 查询违规记录
curl http://localhost:8081/api/admin/compliance/violations?month=2026-05 \
  -H "Authorization: Bearer <admin-token>"
```
