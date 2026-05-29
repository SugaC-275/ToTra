package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BAARecord represents a signed Business Associate Agreement.
type BAARecord struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	SignedByUserID   string    `json:"signed_by_user_id"`
	SignedByName     string    `json:"signed_by_name"`
	SignedByEmail    string    `json:"signed_by_email"`
	SignedAt         time.Time `json:"signed_at"`
	AgreementVersion string    `json:"agreement_version"`
	IPAddress        string    `json:"ip_address,omitempty"`
	IsActive         bool      `json:"is_active"`
}

// BAAStore provides read/write access to BAA agreements.
type BAAStore struct {
	pool *pgxpool.Pool
}

// NewBAAStore creates a BAAStore backed by the given connection pool.
func NewBAAStore(pool *pgxpool.Pool) *BAAStore {
	return &BAAStore{pool: pool}
}

// Sign inserts or replaces a BAA for the given tenant and version.
func (s *BAAStore) Sign(ctx context.Context, tenantID, userID, name, email, version, ip string) (*BAARecord, error) {
	if version == "" {
		version = "v1.0"
	}
	r := &BAARecord{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO baa_agreements
			(tenant_id, signed_by_user_id, signed_by_name, signed_by_email, agreement_version, ip_address, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		ON CONFLICT (tenant_id, agreement_version)
		DO UPDATE SET
			signed_by_user_id  = EXCLUDED.signed_by_user_id,
			signed_by_name     = EXCLUDED.signed_by_name,
			signed_by_email    = EXCLUDED.signed_by_email,
			signed_at          = NOW(),
			ip_address         = EXCLUDED.ip_address,
			is_active          = true
		RETURNING id, tenant_id, signed_by_user_id, signed_by_name, signed_by_email,
		          signed_at, agreement_version, COALESCE(ip_address,''), is_active
	`, tenantID, userID, name, email, version, ip).Scan(
		&r.ID, &r.TenantID, &r.SignedByUserID, &r.SignedByName, &r.SignedByEmail,
		&r.SignedAt, &r.AgreementVersion, &r.IPAddress, &r.IsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("baa sign: %w", err)
	}
	return r, nil
}

// GetActive returns the active BAA for the tenant, or nil if none exists.
func (s *BAAStore) GetActive(ctx context.Context, tenantID string) (*BAARecord, error) {
	r := &BAARecord{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, signed_by_user_id, signed_by_name, signed_by_email,
		       signed_at, agreement_version, COALESCE(ip_address,''), is_active
		FROM baa_agreements
		WHERE tenant_id = $1 AND is_active = true
		ORDER BY signed_at DESC
		LIMIT 1
	`, tenantID).Scan(
		&r.ID, &r.TenantID, &r.SignedByUserID, &r.SignedByName, &r.SignedByEmail,
		&r.SignedAt, &r.AgreementVersion, &r.IPAddress, &r.IsActive,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("baa get active: %w", err)
	}
	return r, nil
}

// List returns all BAA records for the tenant, newest first.
func (s *BAAStore) List(ctx context.Context, tenantID string) ([]*BAARecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, signed_by_user_id, signed_by_name, signed_by_email,
		       signed_at, agreement_version, COALESCE(ip_address,''), is_active
		FROM baa_agreements
		WHERE tenant_id = $1
		ORDER BY signed_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("baa list: %w", err)
	}
	defer rows.Close()
	var records []*BAARecord
	for rows.Next() {
		r := &BAARecord{}
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.SignedByUserID, &r.SignedByName, &r.SignedByEmail,
			&r.SignedAt, &r.AgreementVersion, &r.IPAddress, &r.IsActive,
		); err != nil {
			return nil, fmt.Errorf("baa list scan: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("baa list rows: %w", err)
	}
	if records == nil {
		records = []*BAARecord{}
	}
	return records, nil
}

// Revoke marks a specific BAA record as inactive.
func (s *BAAStore) Revoke(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE baa_agreements SET is_active = false
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("baa revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("baa record not found")
	}
	return nil
}
