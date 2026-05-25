package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BatchJob represents an async batch inference job.
type BatchJob struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	UserID            string         `json:"user_id"`
	ModelName         string         `json:"model"`
	Status            string         `json:"status"`
	TotalRequests     int            `json:"total_requests"`
	CompletedRequests int            `json:"completed_requests"`
	FailedRequests    int            `json:"failed_requests"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
}

// BatchRequestItem is one inference request within a batch.
type BatchRequestItem struct {
	CustomID    string         `json:"custom_id"`
	RequestBody map[string]any `json:"body"`
}

// BatchResultItem is the processed result for one request in a batch.
type BatchResultItem struct {
	CustomID    string         `json:"custom_id"`
	Status      string         `json:"status"`
	Response    map[string]any `json:"response,omitempty"`
	Error       string         `json:"error,omitempty"`
	ProcessedAt *time.Time     `json:"processed_at,omitempty"`
}

// BatchPendingRow holds a single unprocessed request row for the worker.
type BatchPendingRow struct {
	ID          int64
	CustomID    string
	RequestBody []byte
}

// BatchStore handles batch job persistence.
type BatchStore struct {
	pool *pgxpool.Pool
}

func NewBatchStore(pool *pgxpool.Pool) *BatchStore {
	return &BatchStore{pool: pool}
}

// Create inserts a new batch job and its request rows atomically.
func (s *BatchStore) Create(ctx context.Context, tenantID, userID, modelName string,
	requests []BatchRequestItem, metadata map[string]any) (*BatchJob, error) {

	if len(requests) == 0 {
		return nil, fmt.Errorf("batch must contain at least one request")
	}
	if len(requests) > 10_000 {
		return nil, fmt.Errorf("batch exceeds maximum of 10 000 requests")
	}

	metaBytes, _ := json.Marshal(metadata)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var job BatchJob
	err = tx.QueryRow(ctx,
		`INSERT INTO batch_jobs (tenant_id, user_id, model_name, total_requests, metadata)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, tenant_id, user_id, model_name, status,
		           total_requests, completed_requests, failed_requests,
		           metadata, created_at, updated_at`,
		tenantID, userID, modelName, len(requests), metaBytes,
	).Scan(&job.ID, &job.TenantID, &job.UserID, &job.ModelName, &job.Status,
		&job.TotalRequests, &job.CompletedRequests, &job.FailedRequests,
		&job.Metadata, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("batch_jobs insert: %w", err)
	}

	for _, r := range requests {
		bodyBytes, _ := json.Marshal(r.RequestBody)
		_, err = tx.Exec(ctx,
			`INSERT INTO batch_requests (job_id, custom_id, request_body) VALUES ($1, $2, $3)`,
			job.ID, r.CustomID, bodyBytes)
		if err != nil {
			return nil, fmt.Errorf("batch_requests insert (%s): %w", r.CustomID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &job, nil
}

// Get retrieves a batch job by ID, enforcing tenant isolation.
func (s *BatchStore) Get(ctx context.Context, tenantID, jobID string) (*BatchJob, error) {
	var job BatchJob
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, model_name, status,
		        total_requests, completed_requests, failed_requests,
		        metadata, created_at, updated_at, completed_at
		 FROM batch_jobs WHERE id = $1 AND tenant_id = $2`,
		jobID, tenantID,
	).Scan(&job.ID, &job.TenantID, &job.UserID, &job.ModelName, &job.Status,
		&job.TotalRequests, &job.CompletedRequests, &job.FailedRequests,
		&job.Metadata, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// List returns paginated batch jobs for a tenant, newest first.
func (s *BatchStore) List(ctx context.Context, tenantID string, limit, offset int) ([]BatchJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, model_name, status,
		        total_requests, completed_requests, failed_requests,
		        metadata, created_at, updated_at, completed_at
		 FROM batch_jobs WHERE tenant_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []BatchJob
	for rows.Next() {
		var job BatchJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.UserID, &job.ModelName, &job.Status,
			&job.TotalRequests, &job.CompletedRequests, &job.FailedRequests,
			&job.Metadata, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// Cancel marks a queued or processing job as cancelled.
func (s *BatchStore) Cancel(ctx context.Context, tenantID, jobID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE batch_jobs SET status = 'cancelled', updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND status IN ('queued','processing')`,
		jobID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job not found or not cancellable")
	}
	return nil
}

// Results returns processed results for a completed batch job.
func (s *BatchStore) Results(ctx context.Context, tenantID, jobID string, limit, offset int) ([]BatchResultItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT br.custom_id, br.status, br.response_body, br.error_message, br.processed_at
		 FROM batch_requests br
		 JOIN batch_jobs bj ON bj.id = br.job_id
		 WHERE br.job_id = $1 AND bj.tenant_id = $2
		 ORDER BY br.id LIMIT $3 OFFSET $4`,
		jobID, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []BatchResultItem
	for rows.Next() {
		var item BatchResultItem
		var responseBody []byte
		if err := rows.Scan(&item.CustomID, &item.Status, &responseBody, &item.Error, &item.ProcessedAt); err != nil {
			return nil, err
		}
		if len(responseBody) > 0 {
			_ = json.Unmarshal(responseBody, &item.Response)
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

// ClaimPending fetches up to n queued jobs, marking them as processing.
func (s *BatchStore) ClaimPending(ctx context.Context, n int) ([]BatchJob, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE batch_jobs SET status = 'processing', updated_at = NOW()
		 WHERE id IN (
		   SELECT id FROM batch_jobs WHERE status = 'queued'
		   ORDER BY created_at LIMIT $1
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, tenant_id, user_id, model_name, total_requests, metadata`,
		n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []BatchJob
	for rows.Next() {
		var job BatchJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.UserID, &job.ModelName,
			&job.TotalRequests, &job.Metadata); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// PendingRequests fetches unprocessed requests for a job.
func (s *BatchStore) PendingRequests(ctx context.Context, jobID string) ([]BatchPendingRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, custom_id, request_body FROM batch_requests
		 WHERE job_id = $1 AND status = 'pending' ORDER BY id`,
		jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BatchPendingRow
	for rows.Next() {
		var r BatchPendingRow
		if err := rows.Scan(&r.ID, &r.CustomID, &r.RequestBody); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveResult persists a single request result and updates job counters.
func (s *BatchStore) SaveResult(ctx context.Context, jobID string, requestID int64,
	status string, responseBody []byte, errMsg string) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	_, err = tx.Exec(ctx,
		`UPDATE batch_requests SET status = $1, response_body = $2, error_message = $3, processed_at = $4
		 WHERE id = $5`,
		status, responseBody, errMsg, now, requestID)
	if err != nil {
		return err
	}

	if status == "completed" {
		_, err = tx.Exec(ctx,
			`UPDATE batch_jobs SET completed_requests = completed_requests + 1, updated_at = NOW() WHERE id = $1`,
			jobID)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE batch_jobs SET failed_requests = failed_requests + 1, updated_at = NOW() WHERE id = $1`,
			jobID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FinalizeJob marks the job complete or failed based on counters.
func (s *BatchStore) FinalizeJob(ctx context.Context, jobID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE batch_jobs
		 SET status = CASE WHEN failed_requests = 0 THEN 'completed' ELSE 'failed' END,
		     completed_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND status = 'processing'`,
		jobID)
	return err
}
