package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// VirtualKey is an API key with its own governance scope.
type VirtualKey struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name"`
	KeyHash    string    `json:"-"` // never returned to callers
	ModelAlias *string   `json:"model_alias,omitempty"`
	BudgetUSD  *float64  `json:"budget_usd,omitempty"`
	PIIPolicy  string    `json:"pii_policy"`
	BundleIDs  []string  `json:"bundle_ids"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	IsActive   bool      `json:"is_active"`
}

// VirtualKeyStore persists virtual key records in Postgres.
type VirtualKeyStore struct{ pool *pgxpool.Pool }

// NewVirtualKeyStore creates a store backed by the given pool.
func NewVirtualKeyStore(pool *pgxpool.Pool) *VirtualKeyStore {
	return &VirtualKeyStore{pool: pool}
}

// Create inserts a new virtual key record.
func (s *VirtualKeyStore) Create(ctx context.Context, vk *VirtualKey) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO virtual_keys
		  (tenant_id, name, key_hash, model_alias, budget_usd, pii_policy, bundle_ids, expires_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at, is_active
	`, vk.TenantID, vk.Name, vk.KeyHash, vk.ModelAlias, vk.BudgetUSD, vk.PIIPolicy,
		vk.BundleIDs, vk.ExpiresAt, vk.CreatedBy,
	).Scan(&vk.ID, &vk.CreatedAt, &vk.IsActive)
}

// List returns all active virtual keys for a tenant.
func (s *VirtualKeyStore) List(ctx context.Context, tenantID string) ([]*VirtualKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, model_alias, budget_usd,
		       pii_policy, bundle_ids, expires_at, created_by, created_at, is_active
		FROM virtual_keys
		WHERE tenant_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("virtual key list: %w", err)
	}
	defer rows.Close()

	var keys []*VirtualKey
	for rows.Next() {
		var vk VirtualKey
		if err := rows.Scan(
			&vk.ID, &vk.TenantID, &vk.Name, &vk.ModelAlias, &vk.BudgetUSD,
			&vk.PIIPolicy, &vk.BundleIDs, &vk.ExpiresAt, &vk.CreatedBy, &vk.CreatedAt, &vk.IsActive,
		); err != nil {
			return nil, fmt.Errorf("virtual key list scan: %w", err)
		}
		keys = append(keys, &vk)
	}
	return keys, rows.Err()
}

// Revoke soft-deletes a virtual key owned by tenantID.
func (s *VirtualKeyStore) Revoke(ctx context.Context, id, tenantID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE virtual_keys SET is_active = false WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("virtual key revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("virtual key not found")
	}
	return nil
}

// GetByHash implements middleware.VirtualKeyLookup.
// Returns a zero-value VirtualKeyInfo (empty TenantID) when not found.
func (s *VirtualKeyStore) GetByHash(ctx context.Context, keyHash string) (middleware.VirtualKeyInfo, error) {
	vk, err := s.getByHash(ctx, keyHash)
	if err != nil {
		return middleware.VirtualKeyInfo{}, err
	}
	if vk == nil {
		return middleware.VirtualKeyInfo{}, nil
	}
	return middleware.VirtualKeyInfo{
		ID:         vk.ID,
		TenantID:   vk.TenantID,
		Name:       vk.Name,
		ModelAlias: vk.ModelAlias,
		PIIPolicy:  vk.PIIPolicy,
		BundleIDs:  vk.BundleIDs,
	}, nil
}

// getByHash is the internal lookup returning *VirtualKey.
func (s *VirtualKeyStore) getByHash(ctx context.Context, keyHash string) (*VirtualKey, error) {
	var vk VirtualKey
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, key_hash, model_alias, budget_usd,
		       pii_policy, bundle_ids, expires_at, created_by, created_at, is_active
		FROM virtual_keys
		WHERE key_hash = $1 AND is_active = true
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, keyHash).Scan(
		&vk.ID, &vk.TenantID, &vk.Name, &vk.KeyHash, &vk.ModelAlias, &vk.BudgetUSD,
		&vk.PIIPolicy, &vk.BundleIDs, &vk.ExpiresAt, &vk.CreatedBy, &vk.CreatedAt, &vk.IsActive,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("virtual key lookup: %w", err)
	}
	return &vk, nil
}
