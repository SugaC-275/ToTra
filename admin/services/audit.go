package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ComputeRecordHash(payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("audit: marshal payload: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func ComputeChainHash(prevHash, recordHash string) string {
	sum := sha256.Sum256([]byte(prevHash + recordHash))
	return hex.EncodeToString(sum[:])
}

type AuditEntry struct {
	ID         int       `json:"id"`
	TenantID   string    `json:"tenant_id"`
	RecordType string    `json:"record_type"`
	RecordID   string    `json:"record_id"`
	RecordHash string    `json:"record_hash"`
	PrevHash   string    `json:"prev_hash"`
	ChainHash  string    `json:"chain_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

type VerifyResult struct {
	Valid      bool `json:"valid"`
	FirstBadID int  `json:"first_bad_id,omitempty"`
}

type AuditService struct {
	pool *pgxpool.Pool
}

func NewAuditService(pool *pgxpool.Pool) *AuditService {
	return &AuditService{pool: pool}
}

func (s *AuditService) AppendAuditLog(ctx context.Context, tenantID, recordType, recordID string, payload any) error {
	recordHash, err := ComputeRecordHash(payload)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("audit: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var prevHash string
	err = tx.QueryRow(ctx, `
		SELECT chain_hash FROM audit_log
		WHERE tenant_id = $1
		ORDER BY id DESC
		LIMIT 1
		FOR UPDATE`, tenantID,
	).Scan(&prevHash)
	if err != nil {
		prevHash = "genesis"
	}

	chainHash := ComputeChainHash(prevHash, recordHash)

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (tenant_id, record_type, record_id, record_hash, prev_hash, chain_hash)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		tenantID, recordType, recordID, recordHash, prevHash, chainHash,
	)
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *AuditService) GetAuditLog(ctx context.Context, tenantID string, limit int) ([]*AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, record_type, record_id, record_hash, prev_hash, chain_hash, created_at
		FROM audit_log
		WHERE tenant_id = $1
		ORDER BY id DESC
		LIMIT $2`, tenantID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.RecordType, &e.RecordID,
			&e.RecordHash, &e.PrevHash, &e.ChainHash, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *AuditService) VerifyChain(ctx context.Context, tenantID string) (*VerifyResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, record_hash, prev_hash, chain_hash
		FROM audit_log
		WHERE tenant_id = $1
		ORDER BY id ASC`, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var recordHash, prevHash, storedChain string
		if err := rows.Scan(&id, &recordHash, &prevHash, &storedChain); err != nil {
			return nil, err
		}
		computed := ComputeChainHash(prevHash, recordHash)
		if computed != storedChain {
			return &VerifyResult{Valid: false, FirstBadID: id}, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &VerifyResult{Valid: true}, nil
}
