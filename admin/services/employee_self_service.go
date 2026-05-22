package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PIIViolationSummary exposes only safe, non-sensitive fields for self-service.
// The full prompt text is never included.
type PIIViolationSummary struct {
	OccurredAt    time.Time `json:"occurred_at"`
	ViolationType string    `json:"violation_type"`
	ActionTaken   string    `json:"action_taken"`
}

// EmployeeQuotaRequest is the user-facing view of a quota request.
type EmployeeQuotaRequest struct {
	ID          string    `json:"id"`
	NewQuota    int       `json:"requested_tokens"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	ReviewNote  *string   `json:"review_note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// EmployeeSelfService provides employees read-only access to their own data.
type EmployeeSelfService struct {
	pool *pgxpool.Pool
}

// NewEmployeeSelfService creates a new EmployeeSelfService.
func NewEmployeeSelfService(pool *pgxpool.Pool) *EmployeeSelfService {
	return &EmployeeSelfService{pool: pool}
}

// GetMyPIIViolations returns PII violations for the calling user.
// Prompt text is never exposed; only timestamp, type, and action are returned.
func (s *EmployeeSelfService) GetMyPIIViolations(ctx context.Context, tenantID, userID string, limit int) ([]PIIViolationSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT occurred_at, pii_type, action
		FROM pii_violations
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY occurred_at DESC
		LIMIT $3`,
		tenantID, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pii_violations: %w", err)
	}
	defer rows.Close()

	var result []PIIViolationSummary
	for rows.Next() {
		var v PIIViolationSummary
		if err := rows.Scan(&v.OccurredAt, &v.ViolationType, &v.ActionTaken); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// SubmitQuotaRequest inserts a new quota increase request for the calling user.
// Returns the new request ID.
func (s *EmployeeSelfService) SubmitQuotaRequest(ctx context.Context, tenantID, userID string, requestedTokens int64, reason string) (string, error) {
	if requestedTokens <= 0 {
		return "", fmt.Errorf("requested_tokens must be positive")
	}
	if reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO quota_requests
		    (id, tenant_id, user_id, requested_by, new_quota, reason, status, updated_at)
		VALUES ($1, $2, $3, $3, $4, $5, 'pending', NOW())`,
		id, tenantID, userID, requestedTokens, reason,
	)
	if err != nil {
		return "", fmt.Errorf("submit quota request: %w", err)
	}
	return id, nil
}

// GetMyQuotaRequests returns the calling user's quota request history.
func (s *EmployeeSelfService) GetMyQuotaRequests(ctx context.Context, tenantID, userID string) ([]EmployeeQuotaRequest, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, new_quota, reason, status, review_note, created_at
		FROM quota_requests
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY created_at DESC`,
		tenantID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query quota_requests: %w", err)
	}
	defer rows.Close()

	var result []EmployeeQuotaRequest
	for rows.Next() {
		var r EmployeeQuotaRequest
		if err := rows.Scan(&r.ID, &r.NewQuota, &r.Reason, &r.Status, &r.ReviewNote, &r.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ExportUsageCSV generates a CSV of the calling user's usage for a given month (YYYY-MM).
// Columns: date, model, prompt_tokens, completion_tokens, scu_cost, usd_cost
func (s *EmployeeSelfService) ExportUsageCSV(ctx context.Context, tenantID, userID, yearMonth string) ([]byte, error) {
	if _, err := time.Parse("2006-01", yearMonth); err != nil {
		return nil, fmt.Errorf("invalid month format (expected YYYY-MM): %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT r.request_at, mc.name, r.prompt_tokens, r.completion_tokens, r.scu_cost, r.usd_cost
		FROM usage_records r
		JOIN model_configs mc ON mc.id = r.model_config_id
		WHERE r.tenant_id = $1
		  AND r.user_id   = $2
		  AND to_char(r.request_at, 'YYYY-MM') = $3
		ORDER BY r.request_at ASC`,
		tenantID, userID, yearMonth,
	)
	if err != nil {
		return nil, fmt.Errorf("query usage_records: %w", err)
	}
	defer rows.Close()

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"date", "model", "prompt_tokens", "completion_tokens", "scu_cost", "usd_cost"})

	for rows.Next() {
		var (
			requestAt        time.Time
			model            string
			promptTokens     int
			completionTokens int
			scuCost          float64
			usdCost          float64
		)
		if err := rows.Scan(&requestAt, &model, &promptTokens, &completionTokens, &scuCost, &usdCost); err != nil {
			return nil, err
		}
		w.Write([]string{
			requestAt.Format("2006-01-02T15:04:05Z"),
			model,
			strconv.Itoa(promptTokens),
			strconv.Itoa(completionTokens),
			strconv.FormatFloat(scuCost, 'f', 6, 64),
			strconv.FormatFloat(usdCost, 'f', 6, 64),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
