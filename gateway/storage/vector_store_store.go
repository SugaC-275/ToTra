package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VectorStoreRecord mirrors the vector_stores table.
type VectorStoreRecord struct {
	ID            string
	TenantID      string
	UserID        string
	ModelConfigID string
	Name          string
	Status        string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	FileCounts    map[string]int
}

// VectorStoreStore provides CRUD operations against the vector_stores table.
type VectorStoreStore struct{ pool *pgxpool.Pool }

// NewVectorStoreStore returns a new VectorStoreStore backed by pool.
func NewVectorStoreStore(pool *pgxpool.Pool) *VectorStoreStore {
	return &VectorStoreStore{pool: pool}
}

// Create inserts a new vector store metadata record.
func (s *VectorStoreStore) Create(ctx context.Context, vs *VectorStoreRecord) error {
	fileCountsJSON, err := json.Marshal(vs.FileCounts)
	if err != nil {
		return fmt.Errorf("vector_store_store create marshal: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO vector_stores (id, tenant_id, user_id, model_config_id, name, status, expires_at, file_counts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		vs.ID, vs.TenantID, vs.UserID, vs.ModelConfigID, vs.Name, vs.Status, vs.ExpiresAt, fileCountsJSON,
	)
	if err != nil {
		return fmt.Errorf("vector_store_store create: %w", err)
	}
	return nil
}

// Get retrieves a single vector store record by tenant and ID.
func (s *VectorStoreStore) Get(ctx context.Context, tenantID, vsID string) (*VectorStoreRecord, error) {
	var vs VectorStoreRecord
	var fileCountsJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, COALESCE(name,''), status, created_at, expires_at, file_counts
		 FROM vector_stores WHERE id=$1 AND tenant_id=$2`,
		vsID, tenantID,
	).Scan(&vs.ID, &vs.TenantID, &vs.UserID, &vs.ModelConfigID, &vs.Name, &vs.Status, &vs.CreatedAt, &vs.ExpiresAt, &fileCountsJSON)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vector_store_store get: %w", err)
	}
	if len(fileCountsJSON) > 0 {
		_ = json.Unmarshal(fileCountsJSON, &vs.FileCounts)
	}
	return &vs, nil
}

// List returns up to limit vector stores for the tenant, newest first.
func (s *VectorStoreStore) List(ctx context.Context, tenantID string, limit int) ([]*VectorStoreRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, COALESCE(name,''), status, created_at, expires_at, file_counts
		 FROM vector_stores WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("vector_store_store list: %w", err)
	}
	defer rows.Close()
	var stores []*VectorStoreRecord
	for rows.Next() {
		var vs VectorStoreRecord
		var fileCountsJSON []byte
		if err := rows.Scan(&vs.ID, &vs.TenantID, &vs.UserID, &vs.ModelConfigID, &vs.Name, &vs.Status, &vs.CreatedAt, &vs.ExpiresAt, &fileCountsJSON); err != nil {
			return nil, err
		}
		if len(fileCountsJSON) > 0 {
			_ = json.Unmarshal(fileCountsJSON, &vs.FileCounts)
		}
		stores = append(stores, &vs)
	}
	return stores, rows.Err()
}

// Delete removes a vector store record belonging to tenantID.
func (s *VectorStoreStore) Delete(ctx context.Context, tenantID, vsID string) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM vector_stores WHERE id=$1 AND tenant_id=$2`,
		vsID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("vector_store_store delete: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("vector store not found")
	}
	return nil
}
