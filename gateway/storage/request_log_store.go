package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// RequestLog is a single captured gateway request/response record.
type RequestLog struct {
	ID               string
	TenantID         string
	UserID           string
	ModelConfigID    string
	Provider         string
	Model            string
	RequestBody      []byte
	ResponseBody     []byte
	StatusCode       int
	LatencyMS        int
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	Tags             []string
	CreatedAt        time.Time
}

// RequestLogFilter narrows a List query. Zero values mean "no filter".
type RequestLogFilter struct {
	UserID   string
	Model    string
	Provider string
	// Status is "success" (2xx) or "error" (non-2xx). Empty = all.
	Status string
	// Search performs a case-insensitive substring match across request/response bodies.
	Search string
	// Tag filters logs that contain this tag (WHERE tags @> ARRAY[$n]).
	Tag    string
	Limit  int
	Offset int
}

// RequestLogStore persists and retrieves request logs.
type RequestLogStore struct{ pool *pgxpool.Pool }

// NewRequestLogStore creates a new store backed by the given pool.
func NewRequestLogStore(pool *pgxpool.Pool) *RequestLogStore {
	return &RequestLogStore{pool: pool}
}

// Insert writes a single log record. Errors are non-fatal to the caller.
func (s *RequestLogStore) Insert(ctx context.Context, log *RequestLog) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO request_logs
		   (tenant_id, user_id, model_config_id, provider, model,
		    request_body, response_body, status_code, latency_ms,
		    prompt_tokens, completion_tokens, cost_usd)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		log.TenantID, log.UserID, log.ModelConfigID, log.Provider, log.Model,
		log.RequestBody, log.ResponseBody, log.StatusCode, log.LatencyMS,
		log.PromptTokens, log.CompletionTokens, log.CostUSD,
	)
	return err
}

// InsertRaw satisfies middleware.RequestLogInserter.
// It persists the minimal fields captured by the logger middleware, including spend tags.
func (s *RequestLogStore) InsertRaw(ctx context.Context, log *middleware.RawRequestLog) error {
	tags := log.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO request_logs
		   (tenant_id, user_id, request_body, response_body, status_code, latency_ms, tags)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		log.TenantID, log.UserID,
		log.RequestBody, log.ResponseBody,
		log.StatusCode, log.LatencyMS,
		tags,
	)
	return err
}

// List returns paginated logs for a tenant, plus the total count matching the filter.
func (s *RequestLogStore) List(ctx context.Context, tenantID string, f RequestLogFilter) ([]*RequestLog, int, error) {
	args := []any{tenantID}
	conds := []string{"tenant_id = $1"}
	idx := 2

	if f.UserID != "" {
		conds = append(conds, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, f.UserID)
		idx++
	}
	if f.Model != "" {
		conds = append(conds, fmt.Sprintf("model = $%d", idx))
		args = append(args, f.Model)
		idx++
	}
	if f.Provider != "" {
		conds = append(conds, fmt.Sprintf("provider = $%d", idx))
		args = append(args, f.Provider)
		idx++
	}
	switch f.Status {
	case "success":
		conds = append(conds, "status_code >= 200 AND status_code < 300")
	case "error":
		conds = append(conds, "(status_code < 200 OR status_code >= 300)")
	}
	if f.Search != "" {
		pattern := "%" + f.Search + "%"
		conds = append(conds, fmt.Sprintf(
			"(request_body::text ILIKE $%d OR response_body::text ILIKE $%d)", idx, idx))
		args = append(args, pattern)
		idx++
	}
	if f.Tag != "" {
		conds = append(conds, fmt.Sprintf("tags @> ARRAY[$%d]::text[]", idx))
		args = append(args, f.Tag)
		idx++
	}

	where := "WHERE " + strings.Join(conds, " AND ")

	// Total count.
	var total int
	if err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM request_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset

	listArgs := append(args, limit, offset)
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, provider, model,
		        request_body, response_body, status_code, latency_ms,
		        prompt_tokens, completion_tokens, cost_usd, COALESCE(tags, '{}'), created_at
		 FROM request_logs `+where+
			fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1),
		listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []*RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(
			&l.ID, &l.TenantID, &l.UserID, &l.ModelConfigID, &l.Provider, &l.Model,
			&l.RequestBody, &l.ResponseBody, &l.StatusCode, &l.LatencyMS,
			&l.PromptTokens, &l.CompletionTokens, &l.CostUSD, &l.Tags, &l.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		logs = append(logs, &l)
	}
	return logs, total, rows.Err()
}

// Get retrieves a single log by ID, scoped to the tenant.
func (s *RequestLogStore) Get(ctx context.Context, tenantID, id string) (*RequestLog, error) {
	var l RequestLog
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, provider, model,
		        request_body, response_body, status_code, latency_ms,
		        prompt_tokens, completion_tokens, cost_usd, COALESCE(tags, '{}'), created_at
		 FROM request_logs WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	).Scan(
		&l.ID, &l.TenantID, &l.UserID, &l.ModelConfigID, &l.Provider, &l.Model,
		&l.RequestBody, &l.ResponseBody, &l.StatusCode, &l.LatencyMS,
		&l.PromptTokens, &l.CompletionTokens, &l.CostUSD, &l.Tags, &l.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SpendByTag aggregates cost_usd and tokens grouped by tag for a tenant.
type TagSpend struct {
	Tag              string  `json:"tag"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	RequestCount     int64   `json:"request_count"`
}

// AggregateSpendByTag returns spend metrics grouped by tag for a tenant.
func (s *RequestLogStore) AggregateSpendByTag(ctx context.Context, tenantID string) ([]*TagSpend, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tag,
		       COALESCE(SUM(cost_usd), 0)          AS total_cost_usd,
		       COALESCE(SUM(prompt_tokens), 0)      AS prompt_tokens,
		       COALESCE(SUM(completion_tokens), 0)  AS completion_tokens,
		       COUNT(*)                             AS request_count
		FROM request_logs, UNNEST(tags) AS tag
		WHERE tenant_id = $1
		GROUP BY tag
		ORDER BY total_cost_usd DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("spend by tag: %w", err)
	}
	defer rows.Close()

	var result []*TagSpend
	for rows.Next() {
		var ts TagSpend
		if err := rows.Scan(&ts.Tag, &ts.TotalCostUSD, &ts.PromptTokens, &ts.CompletionTokens, &ts.RequestCount); err != nil {
			return nil, fmt.Errorf("spend by tag scan: %w", err)
		}
		result = append(result, &ts)
	}
	return result, rows.Err()
}
