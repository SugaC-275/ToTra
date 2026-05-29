package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/middleware"
)

// GuardrailConfig holds the DB-driven configuration for a single guardrail check.
type GuardrailConfig struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Name         string         `json:"name"`       // "pii_scan" | "injection_detect" | "policy_rules" | "response_pii"
	Enabled      bool           `json:"enabled"`
	Strictness   string         `json:"strictness"` // "permissive" | "standard" | "strict"
	CustomConfig map[string]any `json:"custom_config"`
	BundleID     string         `json:"bundle_id,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

const guardrailCacheTTL = 30 * time.Second

// GuardrailStore manages guardrail configurations in Postgres with a Redis cache.
type GuardrailStore struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewGuardrailStore creates a GuardrailStore.
func NewGuardrailStore(pool *pgxpool.Pool, rdb *redis.Client) *GuardrailStore {
	return &GuardrailStore{pool: pool, rdb: rdb}
}

func guardrailCacheKey(tenantID, name string) string {
	return fmt.Sprintf("guardrail:%s:%s", tenantID, name)
}

// GetGuardrailConfig returns the guardrail config for a tenant+name as a
// *middleware.GuardrailCfg, satisfying middleware.GuardrailConfigGetter.
// Results are cached in Redis with a 30-second TTL.
// Returns (nil, nil) when no config exists (caller should treat as defaults).
func (s *GuardrailStore) GetGuardrailConfig(ctx context.Context, tenantID, name string) (*middleware.GuardrailCfg, error) {
	key := guardrailCacheKey(tenantID, name)

	// Try Redis cache first (stores the full GuardrailConfig for richness).
	if s.rdb != nil {
		cached, err := s.rdb.Get(ctx, key).Bytes()
		if err == nil {
			var full GuardrailConfig
			if jsonErr := json.Unmarshal(cached, &full); jsonErr == nil {
				return &middleware.GuardrailCfg{Enabled: full.Enabled, Strictness: full.Strictness}, nil
			}
		}
	}

	// Fall back to Postgres.
	var cfg GuardrailConfig
	var customRaw []byte
	var bundleID *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at
		 FROM guardrail_configs
		 WHERE tenant_id = $1 AND name = $2`,
		tenantID, name,
	).Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
		&customRaw, &bundleID, &cfg.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("guardrail_store get: %w", err)
	}
	if len(customRaw) > 0 {
		_ = json.Unmarshal(customRaw, &cfg.CustomConfig)
	}
	if bundleID != nil {
		cfg.BundleID = *bundleID
	}

	// Populate Redis cache with full config.
	if s.rdb != nil {
		if b, marshalErr := json.Marshal(&cfg); marshalErr == nil {
			_ = s.rdb.Set(ctx, key, b, guardrailCacheTTL).Err()
		}
	}

	return &middleware.GuardrailCfg{Enabled: cfg.Enabled, Strictness: cfg.Strictness}, nil
}

// GetGuardrailConfigFull returns the full GuardrailConfig (for admin use).
func (s *GuardrailStore) GetGuardrailConfigFull(ctx context.Context, tenantID, name string) (*GuardrailConfig, error) {
	var cfg GuardrailConfig
	var customRaw []byte
	var bundleID *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at
		 FROM guardrail_configs
		 WHERE tenant_id = $1 AND name = $2`,
		tenantID, name,
	).Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
		&customRaw, &bundleID, &cfg.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("guardrail_store get_full: %w", err)
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
		`SELECT id, tenant_id, name, enabled, strictness, custom_config, bundle_id, updated_at
		 FROM guardrail_configs
		 WHERE tenant_id = $1
		 ORDER BY name ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("guardrail_store list: %w", err)
	}
	defer rows.Close()
	var result []*GuardrailConfig
	for rows.Next() {
		var cfg GuardrailConfig
		var customRaw []byte
		var bundleID *string
		if err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
			&customRaw, &bundleID, &cfg.UpdatedAt); err != nil {
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

// UpsertGuardrailConfig inserts or updates a guardrail config and invalidates the cache.
func (s *GuardrailStore) UpsertGuardrailConfig(ctx context.Context, tenantID, name string, enabled bool, strictness string, customConfig map[string]any, bundleID *string) (*GuardrailConfig, error) {
	customRaw, err := json.Marshal(customConfig)
	if err != nil {
		customRaw = []byte("{}")
	}

	var cfg GuardrailConfig
	var rawBundle *string
	var rawCustom []byte
	err = s.pool.QueryRow(ctx,
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
	).Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.Enabled, &cfg.Strictness,
		&rawCustom, &rawBundle, &cfg.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("guardrail_store upsert: %w", err)
	}
	if len(rawCustom) > 0 {
		_ = json.Unmarshal(rawCustom, &cfg.CustomConfig)
	}
	if rawBundle != nil {
		cfg.BundleID = *rawBundle
	}

	// Invalidate Redis cache.
	if s.rdb != nil {
		_ = s.rdb.Del(ctx, guardrailCacheKey(tenantID, name)).Err()
	}

	return &cfg, nil
}

// DeleteGuardrailConfig removes a guardrail config and invalidates the cache.
func (s *GuardrailStore) DeleteGuardrailConfig(ctx context.Context, tenantID, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM guardrail_configs WHERE tenant_id = $1 AND name = $2`,
		tenantID, name,
	)
	if err != nil {
		return fmt.Errorf("guardrail_store delete: %w", err)
	}
	if s.rdb != nil {
		_ = s.rdb.Del(ctx, guardrailCacheKey(tenantID, name)).Err()
	}
	return nil
}
