package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

type PGUserLookup struct{ pool *pgxpool.Pool }

func NewPGUserLookup(pool *pgxpool.Pool) *PGUserLookup {
	return &PGUserLookup{pool: pool}
}

func (p *PGUserLookup) LookupByKeyHash(hash string) (*middleware.UserInfo, error) {
	var u middleware.UserInfo
	var aliasesJSON []byte
	var deptID *string
	err := p.pool.QueryRow(context.Background(),
		`SELECT id, tenant_id, role, model_aliases, department_id
		 FROM users WHERE api_key_hash = $1 AND is_active = true`,
		hash,
	).Scan(&u.UserID, &u.TenantID, &u.Role, &aliasesJSON, &deptID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if len(aliasesJSON) > 0 {
		_ = json.Unmarshal(aliasesJSON, &u.ModelAliases)
	}
	if deptID != nil {
		u.DepartmentID = *deptID
	}
	return &u, nil
}

type PGUserQuota struct{ pool *pgxpool.Pool }

func NewPGUserQuota(pool *pgxpool.Pool) *PGUserQuota { return &PGUserQuota{pool: pool} }

func (p *PGUserQuota) GetUserQuota(ctx context.Context, tenantID, userID string) (int, error) {
	var quota int
	err := p.pool.QueryRow(ctx,
		`SELECT quota_scu FROM users WHERE id = $1 AND tenant_id = $2`,
		userID, tenantID,
	).Scan(&quota)
	if err != nil {
		return 0, fmt.Errorf("get user quota: %w", err)
	}
	return quota, nil
}

type ModelConfig struct {
	ID              string
	Name            string
	Provider        string
	APIKey          string
	BaseURL         string
	SCURate         float64
	CacheDisabled   bool
	BAACompliant    bool     // true = model may process HIPAA PHI
	FINRACompliant  bool     // true = model approved for FINRA-regulated financial data
	SOXAuditEnabled bool     // true = all requests through this model are SOX-audited
	PricePerMInput  *float64 // nil when not configured
	PricePerMOutput *float64 // nil when not configured
	HIPAAEligible   bool     // true = model is HIPAA-eligible (broader than BAA: covers self-hosted)
	GovCloud        bool     // true = model uses a FedRAMP-authorized GovCloud endpoint
	FedRAMPAuth     bool     // true = model has FedRAMP authorization on record
	DataRegion      string   // 'us', 'eu', 'us-gov', 'au', etc. Empty = unspecified.
	ComplianceNotes string   // free-text notes for compliance team
}

type PGModelLookup struct{ pool *pgxpool.Pool }

func NewPGModelLookup(pool *pgxpool.Pool) *PGModelLookup { return &PGModelLookup{pool: pool} }

func (p *PGModelLookup) GetByName(ctx context.Context, tenantID, modelName string) (*ModelConfig, error) {
	var m ModelConfig
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, provider, COALESCE(api_key_encrypted,''), base_url, scu_rate, cache_disabled,
		        baa_compliant, finra_compliant, sox_audit_enabled, price_per_m_input, price_per_m_output,
		        hipaa_eligible, govcloud, fedramp_auth, COALESCE(data_region,''), COALESCE(compliance_notes,'')
		 FROM model_configs WHERE tenant_id = $1 AND name = $2 AND is_active = true`,
		tenantID, modelName,
	).Scan(&m.ID, &m.Name, &m.Provider, &m.APIKey, &m.BaseURL, &m.SCURate, &m.CacheDisabled,
		&m.BAACompliant, &m.FINRACompliant, &m.SOXAuditEnabled, &m.PricePerMInput, &m.PricePerMOutput,
		&m.HIPAAEligible, &m.GovCloud, &m.FedRAMPAuth, &m.DataRegion, &m.ComplianceNotes)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get model config: %w", err)
	}
	return &m, nil
}

// GetFallbackModel returns the first fallback model config for the given primary model.
// Returns (nil, nil) when no fallback is configured.
// Kept for backward compatibility; callers should prefer GetFallbackChain.
func (p *PGModelLookup) GetFallbackModel(ctx context.Context, tenantID, primaryModelName string) (*middleware.ModelConfig, error) {
	chain, err := p.GetFallbackChain(ctx, tenantID, primaryModelName)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, nil
	}
	return chain[0], nil
}

// GetFallbackChain walks the fallback_model_config_id linked list starting from
// primaryModelName and returns each fallback in order (up to 5 hops) to prevent
// infinite loops. Returns an empty slice when no fallback is configured.
func (p *PGModelLookup) GetFallbackChain(ctx context.Context, tenantID, primaryModelName string) ([]*middleware.ModelConfig, error) {
	const maxHops = 5

	// Resolve the ID of the primary model first.
	var primaryID string
	err := p.pool.QueryRow(ctx,
		`SELECT id FROM model_configs WHERE tenant_id = $1 AND name = $2 AND is_active = true`,
		tenantID, primaryModelName,
	).Scan(&primaryID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get fallback chain: resolve primary: %w", err)
	}

	chain := make([]*middleware.ModelConfig, 0, maxHops)
	visited := map[string]struct{}{primaryID: {}}
	currentID := primaryID

	for i := 0; i < maxHops; i++ {
		var m middleware.ModelConfig
		var nextID *string
		err := p.pool.QueryRow(ctx,
			`SELECT fb.id, fb.name, fb.provider, COALESCE(fb.api_key_encrypted,''), fb.base_url, fb.fallback_model_config_id
			 FROM model_configs cur
			 JOIN model_configs fb ON fb.id = cur.fallback_model_config_id
			 WHERE cur.id = $1 AND fb.is_active = true`,
			currentID,
		).Scan(&m.ID, &m.Name, &m.Provider, &m.APIKey, &m.BaseURL, &nextID)
		if err != nil {
			if err == pgx.ErrNoRows {
				// No further fallback — chain ends here.
				break
			}
			return nil, fmt.Errorf("get fallback chain hop %d: %w", i+1, err)
		}

		// Guard against cycles.
		if _, seen := visited[m.ID]; seen {
			break
		}
		visited[m.ID] = struct{}{}
		chain = append(chain, &m)

		if nextID == nil {
			break
		}
		currentID = m.ID
	}

	return chain, nil
}

// GetActiveBundleIDs returns the bundle IDs that are currently active for a tenant.
func (p *PGModelLookup) GetActiveBundleIDs(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT bundle_id FROM compliance_bundle_activations WHERE tenant_id = $1 AND is_active = true`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("get active bundle ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("get active bundle ids scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetFallback sets the fallback_model_config_id of the given model config record.
func (p *PGModelLookup) SetFallback(ctx context.Context, modelConfigID, fallbackModelConfigID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE model_configs SET fallback_model_config_id=$1 WHERE id=$2`,
		fallbackModelConfigID, modelConfigID)
	if err != nil {
		return fmt.Errorf("set fallback: %w", err)
	}
	return nil
}

// ClearFallback sets fallback_model_config_id to NULL for the given model config.
func (p *PGModelLookup) ClearFallback(ctx context.Context, modelConfigID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE model_configs SET fallback_model_config_id=NULL WHERE id=$1`,
		modelConfigID)
	if err != nil {
		return fmt.Errorf("clear fallback: %w", err)
	}
	return nil
}
