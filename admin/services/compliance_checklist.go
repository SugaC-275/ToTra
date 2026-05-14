package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var checklistKeys = []string{
	"data_governance", "human_oversight", "transparency",
	"accuracy_robustness", "record_keeping", "risk_management",
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
	"compliant": true, "partial": true, "non_compliant": true, "not_assessed": true,
}

var validChecklistKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range checklistKeys {
		m[k] = true
	}
	return m
}()

// ChecklistEntry represents a single EU AI Act checklist item.
type ChecklistEntry struct {
	ItemKey     string  `json:"item_key"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Notes       string  `json:"notes"`
	AssessedAt  *string `json:"assessed_at"`
}

// SOC2Report is a point-in-time SOC 2 readiness snapshot.
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

// ChecklistService manages EU AI Act compliance checklist state.
type ChecklistService struct{ pool *pgxpool.Pool }

// NewChecklistService creates a new ChecklistService.
func NewChecklistService(pool *pgxpool.Pool) *ChecklistService { return &ChecklistService{pool: pool} }

// GetChecklist returns all checklist items for a tenant, merging saved state with defaults.
func (s *ChecklistService) GetChecklist(ctx context.Context, tenantID string) ([]*ChecklistEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT item_key, status, notes, assessed_at FROM eu_ai_act_checklist WHERE tenant_id = $1`,
		tenantID)
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

// UpdateItem upserts a checklist item for a tenant.
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
		 SET status=EXCLUDED.status, notes=EXCLUDED.notes, assessed_at=NOW(), updated_at=NOW()`,
		tenantID, itemKey, status, notes)
	return err
}

// ExportViolationsCSV returns a CSV string of PII violations for a tenant/month.
func (s *ChecklistService) ExportViolationsCSV(ctx context.Context, tenantID, yearMonth string) (string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT v.occurred_at, COALESCE(u.email,'unknown'), v.pii_type, v.action, COALESCE(v.request_path,'')
		 FROM pii_violations v LEFT JOIN users u ON u.id=v.user_id
		 WHERE v.tenant_id=$1 AND to_char(v.occurred_at AT TIME ZONE 'UTC','YYYY-MM')=$2
		 ORDER BY v.occurred_at DESC`, tenantID, yearMonth)
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
			csvEscape(email), csvEscape(piiType), csvEscape(action), csvEscape(path))
	}
	return buf.String(), rows.Err()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, `",`) {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// GetSOC2Report returns a SOC 2 readiness snapshot for a tenant.
// Uses chain_hash from audit_log (the tamper-evident hash column).
func (s *ChecklistService) GetSOC2Report(ctx context.Context, tenantID string) (*SOC2Report, error) {
	report := &SOC2Report{
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		TenantID:          tenantID,
		EUAIActTotalItems: len(checklistKeys),
	}
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(chain_hash),'') FROM audit_log WHERE tenant_id=$1`,
		tenantID).Scan(&report.AuditLogEntryCount, &report.AuditLogLastHash)
	cutoff := time.Now().UTC().AddDate(0, -12, 0)
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pii_violations WHERE tenant_id=$1 AND occurred_at>=$2`,
		tenantID, cutoff).Scan(&report.PIIViolations12Months)
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ip_allowlist WHERE tenant_id=$1 AND enabled=true`,
		tenantID).Scan(&report.IPAllowlistCount)
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM eu_ai_act_checklist WHERE tenant_id=$1 AND status='compliant'`,
		tenantID).Scan(&report.EUAIActCompliantCount)
	return report, nil
}
