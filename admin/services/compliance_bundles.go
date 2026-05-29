package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BundlePolicy is a policy rule that a compliance bundle activates.
type BundlePolicy struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

// ComplianceBundle describes a pre-packaged industry compliance configuration.
type ComplianceBundle struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Industry    string         `json:"industry"`
	Policies    []BundlePolicy `json:"policies"`
	RequireBAA  bool           `json:"require_baa"`
	AuditLevel  string         `json:"audit_level"`
	Features    []string       `json:"features"`
}

// BundleActivation records when a bundle was activated for a tenant.
type BundleActivation struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	BundleID    string    `json:"bundle_id"`
	ActivatedBy string    `json:"activated_by"`
	ActivatedAt time.Time `json:"activated_at"`
	IsActive    bool      `json:"is_active"`
}

// AllBundles is the canonical set of supported compliance bundles.
var AllBundles = map[string]ComplianceBundle{
	"healthcare": {
		ID:       "healthcare",
		Name:     "Healthcare (HIPAA)",
		Industry: "Healthcare",
		Description: "HIPAA-aligned controls for healthcare organizations handling " +
			"Protected Health Information (PHI).",
		RequireBAA: true,
		AuditLevel: "strict",
		Features: []string{
			"HIPAA BAA enforcement",
			"PHI detection and blocking",
			"Audit log retention 7 years",
			"BAA-compliant model only",
			"SIEM PII events",
		},
		Policies: []BundlePolicy{
			{Name: "block-ssn", Pattern: `ssn|social security`, Action: "block"},
			{Name: "flag-phi", Pattern: `dob|date of birth|patient id`, Action: "log"},
			{Name: "block-mrn", Pattern: `MRN[-:#\s]?\d{5,10}|medical record.?number`, Action: "block"},
			{Name: "flag-npi", Pattern: `NPI[-:#\s]?\d{10}`, Action: "log"},
			{Name: "flag-icd", Pattern: `ICD-10|ICD10|diagnosis code [A-Z]\d{2}`, Action: "log"},
			{Name: "flag-rx", Pattern: `prescription|Rx #\d+|DEA[-:#\s]?[A-Z]{2}\d{7}`, Action: "log"},
			{Name: "flag-insurance-id", Pattern: `member.?id[-:#\s]?\d{8,12}|group.?number[-:#\s]?\d{6,}`, Action: "log"},
		},
	},
	"legal": {
		ID:       "legal",
		Name:     "Legal & Compliance",
		Industry: "Legal",
		Description: "Controls for law firms and legal departments to protect " +
			"privileged communications and client confidentiality.",
		AuditLevel: "enhanced",
		Features: []string{
			"Client confidentiality enforcement",
			"Data residency controls",
			"Case-level isolation",
			"Attorney-client privilege guardrails",
		},
		Policies: []BundlePolicy{
			{Name: "flag-privileged", Pattern: `privileged.{0,10}confidential|attorney.client privilege|work product`, Action: "log"},
			{Name: "flag-work-product", Pattern: `ATTORNEY WORK PRODUCT|work product doctrine|prepared in anticipation`, Action: "log"},
			{Name: "flag-under-seal", Pattern: `FILED UNDER SEAL|UNDER SEAL|IN CAMERA|sealed document`, Action: "log"},
			{Name: "flag-case-number", Pattern: `Case No\.?\s*\d+|\d{2}-[A-Z]{2}-\d{4,}|\d{4} [A-Z]+ \d{5,}`, Action: "log"},
			{Name: "flag-nda", Pattern: `NON.DISCLOSURE AGREEMENT|NDA|CONFIDENTIALITY AGREEMENT`, Action: "log"},
			{Name: "block-settlement", Pattern: `CONFIDENTIAL SETTLEMENT|settlement amount|settle for \$`, Action: "block"},
		},
	},
	"government": {
		ID:       "government",
		Name:     "Government / FedRAMP",
		Industry: "Government",
		Description: "FedRAMP-aligned controls for government agencies and contractors " +
			"requiring immutable audit trails and strict data controls.",
		AuditLevel: "strict",
		Features: []string{
			"FedRAMP-aligned controls",
			"No data egress to non-approved providers",
			"Immutable audit trail",
			"Multi-factor auth enforcement",
		},
		Policies: []BundlePolicy{
			{Name: "block-classified", Pattern: `TOP SECRET|TS/SCI|SECRET//?NF`, Action: "block"},
			{Name: "block-noforn", Pattern: `NOFORN|REL TO USA`, Action: "block"},
			{Name: "flag-cui", Pattern: `CUI|CONTROLLED UNCLASSIFIED|FOUO|FOR OFFICIAL USE ONLY`, Action: "log"},
			{Name: "flag-itar", Pattern: `ITAR|EXPORT CONTROLLED|EAR|22 CFR|15 CFR`, Action: "log"},
			{Name: "flag-pii-gov", Pattern: `clearance level|security clearance|SCI access|TS clearance`, Action: "log"},
			{Name: "flag-procurement", Pattern: `sole source|LPTA|IDIQ|task order|contract award`, Action: "log"},
		},
	},
	"finance": {
		ID:       "finance",
		Name:     "Financial Services",
		Industry: "Finance",
		Description: "PCI-DSS and SOX-aligned controls for financial institutions " +
			"handling card data and regulated financial information.",
		AuditLevel: "enhanced",
		Features: []string{
			"PCI-DSS card number detection",
			"SOX audit trail",
			"Trade secret protection",
			"Regulatory reporting hooks",
		},
		Policies: []BundlePolicy{
			{Name: "block-card-numbers", Pattern: `\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`, Action: "block"},
			{Name: "block-iban", Pattern: `[A-Z]{2}\d{2}[A-Z0-9]{4}\d{7,19}`, Action: "block"},
			{Name: "flag-swift", Pattern: `\b[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?\b`, Action: "log"},
			{Name: "block-routing", Pattern: `routing.?number[-:#\s]?\d{9}|ABA[-:#\s]?\d{9}`, Action: "block"},
			{Name: "flag-mnpi", Pattern: `material.{0,10}non.public|MNPI|insider.{0,10}information|pre-announcement|earnings guidance`, Action: "log"},
			{Name: "flag-trade-secret", Pattern: `proprietary.{0,10}algorithm|trade secret|confidential.{0,20}financial`, Action: "log"},
		},
	},
	"education": {
		ID:       "education",
		Name:     "Education (FERPA)",
		Industry: "Education",
		Description: "FERPA-aligned controls for educational institutions " +
			"protecting student records and personally identifiable information.",
		AuditLevel: "standard",
		Features: []string{
			"Student PII protection",
			"FERPA data handling",
			"Parental consent tracking",
			"Educator-safe content policies",
		},
		Policies: []BundlePolicy{
			{Name: "flag-student-id", Pattern: `student.?id|SID[-:#\s]?\d{5,9}`, Action: "log"},
			{Name: "flag-grades", Pattern: `GPA|grade point|transcript|academic record|final grade`, Action: "log"},
			{Name: "flag-disciplinary", Pattern: `disciplinary.{0,20}record|suspension|expulsion|academic probation`, Action: "log"},
			{Name: "flag-ferpa", Pattern: `FERPA|education record|student record`, Action: "log"},
			{Name: "flag-minor-pii", Pattern: `date of birth.{0,30}\d{4}|born in \d{4}|age (of )?[5-9]|age 1[0-2]\b`, Action: "log"},
			{Name: "block-ssn-edu", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "block"},
		},
	},
}

// ComplianceBundleService manages bundle activations and policy creation.
type ComplianceBundleService struct {
	pool *pgxpool.Pool
}

// NewComplianceBundleService returns a new ComplianceBundleService.
func NewComplianceBundleService(pool *pgxpool.Pool) *ComplianceBundleService {
	return &ComplianceBundleService{pool: pool}
}

// GetActivation returns the active activation record for the given bundle, or nil.
func (s *ComplianceBundleService) GetActivation(ctx context.Context, tenantID, bundleID string) (*BundleActivation, error) {
	a := &BundleActivation{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, bundle_id, activated_by, activated_at, is_active
		FROM compliance_bundle_activations
		WHERE tenant_id = $1 AND bundle_id = $2 AND is_active = true
	`, tenantID, bundleID).Scan(
		&a.ID, &a.TenantID, &a.BundleID, &a.ActivatedBy, &a.ActivatedAt, &a.IsActive,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bundle get activation: %w", err)
	}
	return a, nil
}

// ListActivations returns all active bundle activations for the tenant.
func (s *ComplianceBundleService) ListActivations(ctx context.Context, tenantID string) ([]*BundleActivation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, bundle_id, activated_by, activated_at, is_active
		FROM compliance_bundle_activations
		WHERE tenant_id = $1 AND is_active = true
		ORDER BY activated_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bundle list: %w", err)
	}
	defer rows.Close()
	var activations []*BundleActivation
	for rows.Next() {
		a := &BundleActivation{}
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.BundleID, &a.ActivatedBy, &a.ActivatedAt, &a.IsActive,
		); err != nil {
			return nil, fmt.Errorf("bundle list scan: %w", err)
		}
		activations = append(activations, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bundle list rows: %w", err)
	}
	if activations == nil {
		activations = []*BundleActivation{}
	}
	return activations, nil
}

// Activate enables a bundle for a tenant: creates policy rules and records activation.
func (s *ComplianceBundleService) Activate(ctx context.Context, tenantID, bundleID, activatedBy string) (*BundleActivation, error) {
	bundle, ok := AllBundles[bundleID]
	if !ok {
		return nil, fmt.Errorf("unknown bundle %q", bundleID)
	}

	snapshot, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("bundle snapshot: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("bundle activate begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Upsert activation record.
	a := &BundleActivation{}
	err = tx.QueryRow(ctx, `
		INSERT INTO compliance_bundle_activations
			(tenant_id, bundle_id, activated_by, is_active, config_snapshot)
		VALUES ($1, $2, $3, true, $4)
		ON CONFLICT (tenant_id, bundle_id)
		DO UPDATE SET
			activated_by     = EXCLUDED.activated_by,
			activated_at     = NOW(),
			is_active        = true,
			config_snapshot  = EXCLUDED.config_snapshot
		RETURNING id, tenant_id, bundle_id, activated_by, activated_at, is_active
	`, tenantID, bundleID, activatedBy, snapshot).Scan(
		&a.ID, &a.TenantID, &a.BundleID, &a.ActivatedBy, &a.ActivatedAt, &a.IsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("bundle activate upsert: %w", err)
	}

	// Insert bundle-defined policy rules (skip duplicates by name+tenant).
	for _, p := range bundle.Policies {
		_, err := tx.Exec(ctx, `
			INSERT INTO tenant_policy_rules (tenant_id, name, pattern, action)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, tenantID, p.Name, p.Pattern, p.Action)
		if err != nil {
			return nil, fmt.Errorf("bundle policy insert %q: %w", p.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("bundle activate commit: %w", err)
	}
	return a, nil
}

// Deactivate marks a bundle activation as inactive.
func (s *ComplianceBundleService) Deactivate(ctx context.Context, tenantID, bundleID string) error {
	if _, ok := AllBundles[bundleID]; !ok {
		return fmt.Errorf("unknown bundle %q", bundleID)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE compliance_bundle_activations
		SET is_active = false
		WHERE tenant_id = $1 AND bundle_id = $2 AND is_active = true
	`, tenantID, bundleID)
	if err != nil {
		return fmt.Errorf("bundle deactivate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("bundle not active for this tenant")
	}
	return nil
}
