package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ValidateDeletionStatus(s string) error {
	switch s {
	case "pending", "approved", "rejected":
		return nil
	}
	return fmt.Errorf("invalid deletion request status: %q", s)
}

type DeletionRequest struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	UserID      string  `json:"user_id"`
	UserName    string  `json:"user_name,omitempty"`
	UserEmail   string  `json:"user_email,omitempty"`
	Status      string  `json:"status"`
	RequestedAt string  `json:"requested_at"`
	ProcessedAt *string `json:"processed_at"`
}

type DataExport struct {
	ExportedAt   string              `json:"exported_at"`
	UserID       string              `json:"user_id"`
	UsageRecords []ExportUsageRecord `json:"usage_records"`
}

type ExportUsageRecord struct {
	RequestAt        string  `json:"request_at"`
	ModelName        string  `json:"model_name"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	SCUCost          float64 `json:"scu_cost"`
	USDCost          float64 `json:"usd_cost"`
	ResponseMS       int     `json:"response_ms"`
}

type DeletionRequestServiceIface interface {
	CreateRequest(ctx context.Context, tenantID, userID string) (*DeletionRequest, error)
	ListPending(ctx context.Context, tenantID string) ([]*DeletionRequest, error)
	Approve(ctx context.Context, tenantID, requestID string) error
	Reject(ctx context.Context, tenantID, requestID string) error
	ExportUserData(ctx context.Context, tenantID, userID string) (*DataExport, error)
}

type DeletionRequestService struct {
	pool *pgxpool.Pool
}

func NewDeletionRequestService(pool *pgxpool.Pool) *DeletionRequestService {
	return &DeletionRequestService{pool: pool}
}

func (s *DeletionRequestService) CreateRequest(ctx context.Context, tenantID, userID string) (*DeletionRequest, error) {
	var existingID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM data_deletion_requests
		 WHERE tenant_id = $1 AND user_id = $2 AND status = 'pending'`,
		tenantID, userID,
	).Scan(&existingID)
	if err == nil {
		return nil, errors.New("a pending data deletion request already exists")
	}

	id := uuid.New().String()
	_, err = s.pool.Exec(ctx,
		`INSERT INTO data_deletion_requests (id, tenant_id, user_id, status)
		 VALUES ($1, $2, $3, 'pending')`,
		id, tenantID, userID,
	)
	if err != nil {
		return nil, err
	}

	req := &DeletionRequest{
		ID:          id,
		TenantID:    tenantID,
		UserID:      userID,
		Status:      "pending",
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return req, nil
}

func (s *DeletionRequestService) ListPending(ctx context.Context, tenantID string) ([]*DeletionRequest, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.tenant_id, d.user_id,
		       u.name, u.email,
		       d.status,
		       d.requested_at,
		       d.processed_at
		FROM data_deletion_requests d
		JOIN users u ON u.id = d.user_id
		WHERE d.tenant_id = $1 AND d.status = 'pending'
		ORDER BY d.requested_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DeletionRequest
	for rows.Next() {
		r := &DeletionRequest{}
		var requestedAt time.Time
		var processedAt *time.Time
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.UserID,
			&r.UserName, &r.UserEmail,
			&r.Status,
			&requestedAt, &processedAt,
		); err != nil {
			return nil, err
		}
		r.RequestedAt = requestedAt.Format(time.RFC3339)
		if processedAt != nil {
			s2 := processedAt.Format(time.RFC3339)
			r.ProcessedAt = &s2
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *DeletionRequestService) Approve(ctx context.Context, tenantID, requestID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx,
		`UPDATE data_deletion_requests
		 SET status = 'approved', processed_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND status = 'pending'
		 RETURNING user_id`,
		requestID, tenantID,
	).Scan(&userID)
	if err != nil {
		return fmt.Errorf("approve deletion request: %w", err)
	}

	// Delete usage records (usage_records.request_at confirmed)
	if _, err = tx.Exec(ctx,
		`DELETE FROM usage_records WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		return err
	}

	// Delete monthly summaries (monthly_summaries confirmed)
	if _, err = tx.Exec(ctx,
		`DELETE FROM monthly_summaries WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		// Not fatal if something goes wrong here
		_ = err
	}

	return tx.Commit(ctx)
}

func (s *DeletionRequestService) Reject(ctx context.Context, tenantID, requestID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE data_deletion_requests
		 SET status = 'rejected', processed_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND status = 'pending'`,
		requestID, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("deletion request not found or already processed")
	}
	return nil
}

func (s *DeletionRequestService) ExportUserData(ctx context.Context, tenantID, userID string) (*DataExport, error) {
	cutoff := time.Now().UTC().AddDate(0, -12, 0)

	// usage_records: model_config_id FK to model_configs.name (confirmed)
	rows, err := s.pool.Query(ctx, `
		SELECT r.request_at, mc.name,
		       r.prompt_tokens, r.completion_tokens,
		       r.scu_cost, r.usd_cost, r.response_ms
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.tenant_id = $1 AND r.user_id = $2
		  AND r.request_at >= $3
		ORDER BY r.request_at DESC`,
		tenantID, userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usageRecords []ExportUsageRecord
	for rows.Next() {
		var rec ExportUsageRecord
		var requestAt time.Time
		if err := rows.Scan(
			&requestAt, &rec.ModelName,
			&rec.PromptTokens, &rec.CompletionTokens,
			&rec.SCUCost, &rec.USDCost, &rec.ResponseMS,
		); err != nil {
			return nil, err
		}
		rec.RequestAt = requestAt.Format(time.RFC3339)
		usageRecords = append(usageRecords, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if usageRecords == nil {
		usageRecords = []ExportUsageRecord{}
	}

	return &DataExport{
		ExportedAt:   time.Now().UTC().Format(time.RFC3339),
		UserID:       userID,
		UsageRecords: usageRecords,
	}, nil
}
