package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UploadedFile struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id,omitempty"`
	UserID        string    `json:"user_id,omitempty"`
	ModelConfigID string    `json:"model_config_id,omitempty"`
	Filename      string    `json:"filename"`
	Purpose       string    `json:"purpose"`
	Bytes         int64     `json:"bytes"`
	Status        string    `json:"status"`
	Provider      string    `json:"provider"`
	CreatedAt     time.Time `json:"created_at"`
}

type FilesStore struct{ pool *pgxpool.Pool }

func NewFilesStore(pool *pgxpool.Pool) *FilesStore {
	return &FilesStore{pool: pool}
}

func (s *FilesStore) Create(ctx context.Context, f *UploadedFile) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO uploaded_files (id, tenant_id, user_id, model_config_id, filename, purpose, bytes, status, provider)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		f.ID, f.TenantID, f.UserID, f.ModelConfigID, f.Filename, f.Purpose, f.Bytes, f.Status, f.Provider,
	)
	if err != nil {
		return fmt.Errorf("files_store create: %w", err)
	}
	return nil
}

func (s *FilesStore) Get(ctx context.Context, tenantID, fileID string) (*UploadedFile, error) {
	var f UploadedFile
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, filename, purpose, bytes, status, provider, created_at
		 FROM uploaded_files WHERE id=$1 AND tenant_id=$2`,
		fileID, tenantID,
	).Scan(&f.ID, &f.TenantID, &f.UserID, &f.ModelConfigID, &f.Filename, &f.Purpose, &f.Bytes, &f.Status, &f.Provider, &f.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("files_store get: %w", err)
	}
	return &f, nil
}

func (s *FilesStore) List(ctx context.Context, tenantID, purpose string, limit int) ([]*UploadedFile, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := `SELECT id, tenant_id, user_id, model_config_id, filename, purpose, bytes, status, provider, created_at
	           FROM uploaded_files WHERE tenant_id=$1`
	args := []any{tenantID}
	if purpose != "" {
		args = append(args, purpose, limit)
		query += " AND purpose=$2 ORDER BY created_at DESC LIMIT $3"
	} else {
		args = append(args, limit)
		query += " ORDER BY created_at DESC LIMIT $2"
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("files_store list: %w", err)
	}
	defer rows.Close()
	var files []*UploadedFile
	for rows.Next() {
		var f UploadedFile
		if err := rows.Scan(&f.ID, &f.TenantID, &f.UserID, &f.ModelConfigID, &f.Filename, &f.Purpose, &f.Bytes, &f.Status, &f.Provider, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

func (s *FilesStore) Delete(ctx context.Context, tenantID, fileID string) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM uploaded_files WHERE id=$1 AND tenant_id=$2`,
		fileID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("files_store delete: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}
