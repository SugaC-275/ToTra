package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// guardrailRow is the full DB row including updated_at.
type guardrailRow struct {
	GuardrailConfig
	UpdatedAt time.Time `json:"updated_at"`
}

// GuardrailStore provides CRUD for guardrail_configs backed by Postgres.
type GuardrailStore struct {
	pool *pgxpool.Pool
}

// NewGuardrailStore creates a GuardrailStore.
func NewGuardrailStore(pool *pgxpool.Pool) *GuardrailStore {
	return &GuardrailStore{pool: pool}
}

const guardrailSelect = `SELECT id, tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at FROM guardrail_configs`

func scanGuardrail(row pgx.Row) (*GuardrailConfig, error) {
	var cfg GuardrailConfig
	var customRaw []byte
	var bundleID *string
	var updatedAt time.Time
	if err := row.Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
		&customRaw, &bundleID, &updatedAt); err != nil {
		return nil, err
	}
	if len(customRaw) > 0 {
		_ = json.Unmarshal(customRaw, &cfg.CustomConfig)
	}
	if bundleID != nil {
		cfg.BundleID = *bundleID
	}
	return &cfg, nil
}

// ListGuardrailConfigs returns all guardrail configs for a tenant.
func (s *GuardrailStore) ListGuardrailConfigs(ctx context.Context, tenantID string) ([]*GuardrailConfig, error) {
	rows, err := s.pool.Query(ctx,
		guardrailSelect+` WHERE tenant_id = $1 ORDER BY name ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("guardrail list: %w", err)
	}
	defer rows.Close()
	var result []*GuardrailConfig
	for rows.Next() {
		var cfg GuardrailConfig
		var customRaw []byte
		var bundleID *string
		var updatedAt time.Time
		if err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
			&customRaw, &bundleID, &updatedAt); err != nil {
			return nil, err
		}
		if len(customRaw) > 0 {
			_ = json.Unmarshal(customRaw, &cfg.CustomConfig)
		}
		if bundleID != nil {
			cfg.BundleID = *bundleID
		}
		result = append(result, &cfg)
	}
	return result, rows.Err()
}

// UpsertGuardrailConfig inserts or updates a guardrail config.
func (s *GuardrailStore) UpsertGuardrailConfig(ctx context.Context, tenantID, name string, enabled bool, strictness string, customConfig map[string]any, bundleID *string) (*GuardrailConfig, error) {
	customRaw, err := json.Marshal(customConfig)
	if err != nil {
		customRaw = []byte("{}")
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO guardrail_configs (tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (tenant_id, name) DO UPDATE
		   SET enabled = EXCLUDED.enabled,
		       strictness = EXCLUDED.strictness,
		       custom_config = EXCLUDED.custom_config,
		       bundle_id = EXCLUDED.bundle_id,
		       updated_at = NOW()
		 RETURNING id, tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at`,
		tenantID, name, enabled, strictness, customRaw, bundleID,
	)
	cfg, err := scanGuardrail(row)
	if err != nil {
		return nil, fmt.Errorf("guardrail upsert: %w", err)
	}
	return cfg, nil
}

// DeleteGuardrailConfig removes a guardrail config.
func (s *GuardrailStore) DeleteGuardrailConfig(ctx context.Context, tenantID, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM guardrail_configs WHERE tenant_id = $1 AND name = $2`, tenantID, name)
	if err != nil {
		return fmt.Errorf("guardrail delete: %w", err)
	}
	return nil
}
