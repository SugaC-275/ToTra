package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QuotaRequest struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	NewQuota    int    `json:"new_quota"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
}

type QuotaStatus struct {
	QuotaSCU int     `json:"quota_scu"`
	UsedSCU  float64 `json:"used_scu"`
}

type QuotaServiceInterface interface {
	RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error
	ListPending(ctx context.Context, tenantID string) ([]*QuotaRequest, error)
	Approve(ctx context.Context, tenantID, requestID, reviewerID string) error
	Reject(ctx context.Context, tenantID, requestID, reviewerID string) error
	GetMyStatus(ctx context.Context, tenantID, userID, month string) (*QuotaStatus, error)
}

type QuotaService struct{ pool *pgxpool.Pool }

func NewQuotaService(pool *pgxpool.Pool) *QuotaService { return &QuotaService{pool: pool} }

func (s *QuotaService) RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO quota_requests (id, tenant_id, user_id, requested_by, new_quota, reason) VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.New().String(), tenantID, userID, requestedBy, newQuota, reason,
	)
	return err
}

func (s *QuotaService) ListPending(ctx context.Context, tenantID string) ([]*QuotaRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, new_quota, reason, status, requested_by FROM quota_requests WHERE tenant_id=$1 AND status='pending' ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reqs []*QuotaRequest
	for rows.Next() {
		r := &QuotaRequest{}
		rows.Scan(&r.ID, &r.UserID, &r.NewQuota, &r.Reason, &r.Status, &r.RequestedBy)
		reqs = append(reqs, r)
	}
	return reqs, nil
}

func (s *QuotaService) Approve(ctx context.Context, tenantID, requestID, reviewerID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	var newQuota int
	err = tx.QueryRow(ctx,
		`UPDATE quota_requests SET status='approved', reviewed_by=$1, reviewed_at=NOW()
		 WHERE id=$2 AND tenant_id=$3 AND status='pending' RETURNING user_id, new_quota`,
		reviewerID, requestID, tenantID,
	).Scan(&userID, &newQuota)
	if err != nil {
		return fmt.Errorf("approve quota request: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE users SET quota_scu=$1 WHERE id=$2`, newQuota, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *QuotaService) Reject(ctx context.Context, tenantID, requestID, reviewerID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE quota_requests SET status='rejected', reviewed_by=$1, reviewed_at=NOW()
		 WHERE id=$2 AND tenant_id=$3 AND status='pending'`,
		reviewerID, requestID, tenantID,
	)
	return err
}

func (s *QuotaService) GetMyStatus(ctx context.Context, tenantID, userID, month string) (*QuotaStatus, error) {
	var qs QuotaStatus
	err := s.pool.QueryRow(ctx, `
		SELECT u.quota_scu, COALESCE(SUM(r.scu_cost), 0)
		FROM users u
		LEFT JOIN usage_records r ON r.user_id = u.id
		    AND to_char(r.request_at, 'YYYY-MM') = $3
		WHERE u.id = $2 AND u.tenant_id = $1
		GROUP BY u.quota_scu`,
		tenantID, userID, month,
	).Scan(&qs.QuotaSCU, &qs.UsedSCU)
	return &qs, err
}
