package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// PolicyEventStore persists policy match events to the policy_events table.
type PolicyEventStore struct{ pool *pgxpool.Pool }

// NewPolicyEventStore creates a store backed by the given pool.
func NewPolicyEventStore(pool *pgxpool.Pool) *PolicyEventStore {
	return &PolicyEventStore{pool: pool}
}

// InsertPolicyEvent implements middleware.PolicyEventInserter.
func (s *PolicyEventStore) InsertPolicyEvent(ctx context.Context, ev middleware.PolicyEventRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO policy_events (tenant_id, user_id, rule_name, action, matched_pattern, request_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, ev.TenantID, ev.UserID, ev.RuleName, ev.Action, ev.MatchedPattern, ev.RequestID)
	if err != nil {
		return fmt.Errorf("policy event insert: %w", err)
	}
	return nil
}
