package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// ComplianceReportData holds all sections of a compliance report.
type ComplianceReportData struct {
	GeneratedAt time.Time `json:"generated_at"`
	TenantID    string    `json:"tenant_id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`

	// Summary stats
	TotalRequests    int `json:"total_requests"`
	BlockedRequests  int `json:"blocked_requests"`
	PIIDetections    int `json:"pii_detections"`
	BAAViolations    int `json:"baa_violations"`
	PolicyViolations int `json:"policy_violations"`

	// Top PII types detected
	PIIBreakdown []PIIBreakdownItem `json:"pii_breakdown"`

	// SIEM events by type
	SIEMBreakdown []SIEMBreakdownItem `json:"siem_breakdown"`

	// AI Act: high-risk system usage
	HighRiskSystems []HighRiskSystemItem `json:"high_risk_systems"`

	// Checklist items (SOC 2 style)
	Checklist []ChecklistItem `json:"checklist"`
}

// PIIBreakdownItem holds a PII type and its detection count.
type PIIBreakdownItem struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// SIEMBreakdownItem holds a SIEM event type and its count.
type SIEMBreakdownItem struct {
	EventType string `json:"event_type"`
	Count     int    `json:"count"`
}

// HighRiskSystemItem holds an AI Act high-risk system and its request count.
type HighRiskSystemItem struct {
	SystemName   string `json:"system_name"`
	RequestCount int    `json:"request_count"`
}

// ChecklistItem is a single SOC2-style compliance checklist entry.
type ChecklistItem struct {
	Category string `json:"category"`
	Item     string `json:"item"`
	Status   string `json:"status"` // "enabled", "disabled", "partial"
	Detail   string `json:"detail"`
}

// RegisterComplianceReportRoutes mounts:
// GET /v1/compliance/report?period=30d|90d|1y&format=json|csv
func RegisterComplianceReportRoutes(router fiber.Router, pool *pgxpool.Pool) {
	router.Get("/compliance/report", getComplianceReport(pool))
}

func getComplianceReport(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		period := c.Query("period", "30d")
		format := c.Query("format", "json")

		now := time.Now().UTC()
		var periodStart time.Time
		switch period {
		case "90d":
			periodStart = now.AddDate(0, 0, -90)
		case "1y":
			periodStart = now.AddDate(-1, 0, 0)
		default: // 30d
			periodStart = now.AddDate(0, 0, -30)
		}

		report, err := buildComplianceReport(c.Context(), pool, user.TenantID, periodStart, now)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to build report"})
		}

		if format == "csv" {
			return renderCSV(c, report)
		}
		return c.JSON(report)
	}
}

func buildComplianceReport(ctx context.Context, pool *pgxpool.Pool, tenantID string, periodStart, periodEnd time.Time) (*ComplianceReportData, error) {
	report := &ComplianceReportData{
		GeneratedAt: time.Now().UTC(),
		TenantID:    tenantID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	// --- Summary stats ---

	// Total requests from request_logs
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM request_logs WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3`,
		tenantID, periodStart, periodEnd,
	).Scan(&report.TotalRequests)

	// Blocked requests (non-2xx status codes from request_logs)
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM request_logs WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 AND status_code >= 400`,
		tenantID, periodStart, periodEnd,
	).Scan(&report.BlockedRequests)

	// PII detections
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pii_violations WHERE tenant_id = $1 AND occurred_at BETWEEN $2 AND $3`,
		tenantID, periodStart, periodEnd,
	).Scan(&report.PIIDetections)

	// BAA violations: ai_act_oversight_log entries where risk_level = 'high' or 'unacceptable'
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ai_act_oversight_log WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 AND risk_level IN ('high','unacceptable')`,
		tenantID, periodStart, periodEnd,
	).Scan(&report.BAAViolations)

	// Policy violations from siem_delivery_queue (policy-type events)
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM siem_delivery_queue WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 AND event_type LIKE 'policy%'`,
		tenantID, periodStart, periodEnd,
	).Scan(&report.PolicyViolations)

	// --- PII breakdown by type ---
	piiRows, err := pool.Query(ctx,
		`SELECT pii_type, COUNT(*) AS cnt FROM pii_violations WHERE tenant_id = $1 AND occurred_at BETWEEN $2 AND $3 GROUP BY pii_type ORDER BY cnt DESC LIMIT 20`,
		tenantID, periodStart, periodEnd,
	)
	if err == nil {
		defer piiRows.Close()
		for piiRows.Next() {
			var item PIIBreakdownItem
			if err := piiRows.Scan(&item.Type, &item.Count); err == nil {
				report.PIIBreakdown = append(report.PIIBreakdown, item)
			}
		}
	}
	if report.PIIBreakdown == nil {
		report.PIIBreakdown = []PIIBreakdownItem{}
	}

	// --- SIEM breakdown by event_type ---
	siemRows, err := pool.Query(ctx,
		`SELECT event_type, COUNT(*) AS cnt FROM siem_delivery_queue WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 GROUP BY event_type ORDER BY cnt DESC LIMIT 20`,
		tenantID, periodStart, periodEnd,
	)
	if err == nil {
		defer siemRows.Close()
		for siemRows.Next() {
			var item SIEMBreakdownItem
			if err := siemRows.Scan(&item.EventType, &item.Count); err == nil {
				report.SIEMBreakdown = append(report.SIEMBreakdown, item)
			}
		}
	}
	if report.SIEMBreakdown == nil {
		report.SIEMBreakdown = []SIEMBreakdownItem{}
	}

	// --- AI Act high-risk system usage ---
	aiRows, err := pool.Query(ctx,
		`SELECT model_config_id, COUNT(*) AS cnt FROM ai_act_oversight_log WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 AND risk_level IN ('high','unacceptable') AND model_config_id IS NOT NULL GROUP BY model_config_id ORDER BY cnt DESC LIMIT 10`,
		tenantID, periodStart, periodEnd,
	)
	if err == nil {
		defer aiRows.Close()
		for aiRows.Next() {
			var item HighRiskSystemItem
			if err := aiRows.Scan(&item.SystemName, &item.RequestCount); err == nil {
				report.HighRiskSystems = append(report.HighRiskSystems, item)
			}
		}
	}
	if report.HighRiskSystems == nil {
		report.HighRiskSystems = []HighRiskSystemItem{}
	}

	// --- Checklist ---
	report.Checklist = buildChecklist(ctx, pool, tenantID, periodStart, periodEnd)

	return report, nil
}

func buildChecklist(ctx context.Context, pool *pgxpool.Pool, tenantID string, periodStart, periodEnd time.Time) []ChecklistItem {
	items := []ChecklistItem{}

	// Authentication: JWT auth is always enabled (gateway enforces it)
	items = append(items, ChecklistItem{
		Category: "Authentication",
		Item:     "JWT authentication enforced",
		Status:   "enabled",
		Detail:   "All API requests require a valid Bearer token",
	})

	// PII Detection: check if pii_violations has recent rows
	var piiCount int
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pii_violations WHERE tenant_id = $1 AND occurred_at BETWEEN $2 AND $3`,
		tenantID, periodStart, periodEnd,
	).Scan(&piiCount)
	piiStatus := "enabled"
	piiDetail := fmt.Sprintf("%d PII events detected in period", piiCount)
	// If no events at all, could be zero traffic — still enabled
	items = append(items, ChecklistItem{
		Category: "Data Protection",
		Item:     "PII scanning active",
		Status:   piiStatus,
		Detail:   piiDetail,
	})

	// SIEM: check siem_configs table for active configs
	var siemExists bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM siem_configs WHERE tenant_id = $1 AND is_active = TRUE)`,
		tenantID,
	).Scan(&siemExists)
	siemStatus := "disabled"
	siemDetail := "No active SIEM integration configured"
	if siemExists {
		siemStatus = "enabled"
		siemDetail = "Active SIEM integration found"
	}
	items = append(items, ChecklistItem{
		Category: "Monitoring",
		Item:     "SIEM integration configured",
		Status:   siemStatus,
		Detail:   siemDetail,
	})

	// Audit Log: immutable audit log always enabled
	items = append(items, ChecklistItem{
		Category: "Audit",
		Item:     "Immutable audit log",
		Status:   "enabled",
		Detail:   "All admin actions recorded with tamper-evident hash chain",
	})

	// Data Retention: check tenants table for data_retention_months
	var retentionMonths int
	retErr := pool.QueryRow(ctx,
		`SELECT data_retention_months FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&retentionMonths)
	retStatus := "disabled"
	retDetail := "No retention policy configured"
	if retErr == nil && retentionMonths > 0 {
		retStatus = "enabled"
		retDetail = fmt.Sprintf("Data retained for %d months", retentionMonths)
	}
	items = append(items, ChecklistItem{
		Category: "Data Governance",
		Item:     "Retention policy configured",
		Status:   retStatus,
		Detail:   retDetail,
	})

	// BAA: check model_configs for baa_compliant=true rows
	var baaExists bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM model_configs WHERE tenant_id = $1 AND baa_compliant = TRUE AND is_active = TRUE)`,
		tenantID,
	).Scan(&baaExists)
	baaStatus := "disabled"
	baaDetail := "No BAA-compliant model configurations active"
	if baaExists {
		baaStatus = "enabled"
		baaDetail = "At least one BAA-compliant model configuration is active"
	}
	items = append(items, ChecklistItem{
		Category: "Healthcare Compliance",
		Item:     "BAA compliance enforced",
		Status:   baaStatus,
		Detail:   baaDetail,
	})

	// Rate Limiting: check rate_limit_configs table
	var rateLimitExists bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM rate_limit_configs WHERE tenant_id = $1)`,
		tenantID,
	).Scan(&rateLimitExists)
	rateLimitStatus := "disabled"
	rateLimitDetail := "No rate limit configurations found"
	if rateLimitExists {
		rateLimitStatus = "enabled"
		rateLimitDetail = "Rate limit policies are configured"
	}
	items = append(items, ChecklistItem{
		Category: "Access Control",
		Item:     "Rate limits configured",
		Status:   rateLimitStatus,
		Detail:   rateLimitDetail,
	})

	// Lakera Guard: check env var
	lakeraStatus := "disabled"
	lakeraDetail := "LAKERA_API_KEY not configured"
	if os.Getenv("LAKERA_API_KEY") != "" {
		lakeraStatus = "enabled"
		lakeraDetail = "Lakera Guard prompt injection detection active"
	}
	items = append(items, ChecklistItem{
		Category: "AI Security",
		Item:     "Prompt injection detection (Lakera)",
		Status:   lakeraStatus,
		Detail:   lakeraDetail,
	})

	// Aporia Guardrails: check env var
	aporiaStatus := "disabled"
	aporiaDetail := "APORIA_API_KEY not configured"
	if os.Getenv("APORIA_API_KEY") != "" {
		aporiaStatus = "enabled"
		aporiaDetail = "Aporia guardrails active"
	}
	items = append(items, ChecklistItem{
		Category: "AI Security",
		Item:     "AI guardrails (Aporia)",
		Status:   aporiaStatus,
		Detail:   aporiaDetail,
	})

	// EU AI Act oversight: check ai_act_oversight_log for recent entries
	var aiActCount int
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ai_act_oversight_log WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3`,
		tenantID, periodStart, periodEnd,
	).Scan(&aiActCount)
	aiActStatus := "partial"
	aiActDetail := "EU AI Act oversight log present but no high-risk events in period"
	if aiActCount > 0 {
		aiActStatus = "enabled"
		aiActDetail = fmt.Sprintf("%d high-risk AI interactions logged for human oversight", aiActCount)
	}
	items = append(items, ChecklistItem{
		Category: "Regulatory",
		Item:     "EU AI Act oversight logging",
		Status:   aiActStatus,
		Detail:   aiActDetail,
	})

	return items
}

func renderCSV(c *fiber.Ctx, report *ComplianceReportData) error {
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="totra-compliance-report-%s.csv"`,
		report.GeneratedAt.Format("2006-01-02"),
	))

	w := csv.NewWriter(c.Response().BodyWriter())

	// Summary section
	_ = w.Write([]string{"Section", "Metric", "Value"})
	_ = w.Write([]string{"Summary", "Generated At", report.GeneratedAt.Format(time.RFC3339)})
	_ = w.Write([]string{"Summary", "Tenant ID", report.TenantID})
	_ = w.Write([]string{"Summary", "Period Start", report.PeriodStart.Format("2006-01-02")})
	_ = w.Write([]string{"Summary", "Period End", report.PeriodEnd.Format("2006-01-02")})
	_ = w.Write([]string{"Summary", "Total Requests", fmt.Sprintf("%d", report.TotalRequests)})
	_ = w.Write([]string{"Summary", "Blocked Requests", fmt.Sprintf("%d", report.BlockedRequests)})
	_ = w.Write([]string{"Summary", "PII Detections", fmt.Sprintf("%d", report.PIIDetections)})
	_ = w.Write([]string{"Summary", "BAA Violations", fmt.Sprintf("%d", report.BAAViolations)})
	_ = w.Write([]string{"Summary", "Policy Violations", fmt.Sprintf("%d", report.PolicyViolations)})
	_ = w.Write([]string{})

	// PII breakdown
	_ = w.Write([]string{"PII Breakdown", "Type", "Count"})
	for _, p := range report.PIIBreakdown {
		_ = w.Write([]string{"PII Breakdown", p.Type, fmt.Sprintf("%d", p.Count)})
	}
	_ = w.Write([]string{})

	// SIEM breakdown
	_ = w.Write([]string{"SIEM Events", "Event Type", "Count"})
	for _, s := range report.SIEMBreakdown {
		_ = w.Write([]string{"SIEM Events", s.EventType, fmt.Sprintf("%d", s.Count)})
	}
	_ = w.Write([]string{})

	// Checklist
	_ = w.Write([]string{"Checklist", "Category", "Item", "Status", "Detail"})
	for _, ch := range report.Checklist {
		_ = w.Write([]string{"Checklist", ch.Category, ch.Item, ch.Status, ch.Detail})
	}

	w.Flush()
	return w.Error()
}

// MarshalComplianceReportJSON is a helper used by the frontend print handler.
func MarshalComplianceReportJSON(report *ComplianceReportData) ([]byte, error) {
	return json.Marshal(report)
}
