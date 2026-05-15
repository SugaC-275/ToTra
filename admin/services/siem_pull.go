package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SIEMEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TenantID   string          `json:"tenant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Detail     json.RawMessage `json:"detail"`
}

type SIEMEventsResult struct {
	Events    []*SIEMEvent `json:"events"`
	NextSince time.Time    `json:"next_since"`
}

type SIEMPullService struct{ pool *pgxpool.Pool }

func NewSIEMPullService(pool *pgxpool.Pool) *SIEMPullService {
	return &SIEMPullService{pool: pool}
}

func (s *SIEMPullService) GetEvents(ctx context.Context, tenantID string, since time.Time, types []string, limit int) (*SIEMEventsResult, error) {
	if len(types) == 0 {
		types = []string{"pii_violation", "policy_block", "audit_log", "quota_exceeded", "routing_event"}
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	// pii_violations.tenant_id is UUID; audit_log and gateway_routing_events use TEXT.
	// We cast $1 to UUID only in the pii_violations branch and keep it as TEXT elsewhere.
	const q = `
SELECT id::text, 'pii_violation' AS type, tenant_id::text, occurred_at,
       jsonb_build_object('user_id', user_id::text, 'pii_type', pii_type, 'action', action, 'path', request_path) AS detail
FROM pii_violations
WHERE tenant_id = $1::uuid AND occurred_at >= $2 AND 'pii_violation' = ANY($3::text[])

UNION ALL

SELECT id::text, 'audit_log', tenant_id, created_at,
       jsonb_build_object('record_type', record_type, 'record_id', record_id) AS detail
FROM audit_log
WHERE tenant_id = $1 AND created_at >= $2 AND 'audit_log' = ANY($3::text[])

UNION ALL

SELECT id::text, 'routing_event', tenant_id, routed_at,
       jsonb_build_object('original_model', original_model, 'routed_model', routed_model) AS detail
FROM gateway_routing_events
WHERE tenant_id = $1 AND routed_at >= $2 AND 'routing_event' = ANY($3::text[])

ORDER BY occurred_at DESC
LIMIT $4
`

	rows, err := s.pool.Query(ctx, q, tenantID, since, types, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &SIEMEventsResult{Events: []*SIEMEvent{}, NextSince: since}
	for rows.Next() {
		var ev SIEMEvent
		var detailJSON []byte
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.TenantID, &ev.OccurredAt, &detailJSON); err != nil {
			return nil, err
		}
		ev.Detail = json.RawMessage(detailJSON)
		result.Events = append(result.Events, &ev)
		if ev.OccurredAt.After(result.NextSince) {
			result.NextSince = ev.OccurredAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
