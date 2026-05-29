package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CacheConfig holds per-tenant cache TTL and enablement settings.
type CacheConfig struct {
	TenantID        string
	ExactTTL        int  // seconds
	SemanticTTL     int  // seconds
	SemanticEnabled bool
}

// defaultCacheConfig returns the system default config for a tenant that has
// no row in cache_configs.
func defaultCacheConfig(tenantID string) *CacheConfig {
	return &CacheConfig{
		TenantID:        tenantID,
		ExactTTL:        3600,
		SemanticTTL:     7200,
		SemanticEnabled: true,
	}
}

// CacheConfigStore persists per-tenant cache configuration in Postgres.
type CacheConfigStore struct{ pool *pgxpool.Pool }

func NewCacheConfigStore(pool *pgxpool.Pool) *CacheConfigStore {
	return &CacheConfigStore{pool: pool}
}

// Get returns the cache config for tenantID. If no row exists, returns the
// system default so callers never receive nil.
func (s *CacheConfigStore) Get(ctx context.Context, tenantID string) (*CacheConfig, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT exact_ttl_seconds, semantic_ttl_seconds, semantic_enabled
		   FROM cache_configs
		  WHERE tenant_id = $1`,
		tenantID,
	)
	cfg := &CacheConfig{TenantID: tenantID}
	err := row.Scan(&cfg.ExactTTL, &cfg.SemanticTTL, &cfg.SemanticEnabled)
	if err != nil {
		// No row: return default.
		return defaultCacheConfig(tenantID), nil
	}
	return cfg, nil
}

// Set upserts cache config for tenantID.
func (s *CacheConfigStore) Set(ctx context.Context, tenantID string, exactTTL, semanticTTL int, enabled bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cache_configs (tenant_id, exact_ttl_seconds, semantic_ttl_seconds, semantic_enabled, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (tenant_id) DO UPDATE
		   SET exact_ttl_seconds    = EXCLUDED.exact_ttl_seconds,
		       semantic_ttl_seconds = EXCLUDED.semantic_ttl_seconds,
		       semantic_enabled     = EXCLUDED.semantic_enabled,
		       updated_at           = NOW()`,
		tenantID, exactTTL, semanticTTL, enabled,
	)
	return err
}
