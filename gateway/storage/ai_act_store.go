package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AIActOversightRecord holds the data written to ai_act_oversight_log.
type AIActOversightRecord struct {
	TenantID      string
	UserID        string
	RiskLevel     string
	RiskReasons   []string
	ModelConfigID string
	RequestHash   string
}

// AIActStore persists EU AI Act oversight log entries to Postgres.
type AIActStore struct{ pool *pgxpool.Pool }

// NewAIActStore creates a new AIActStore backed by the given connection pool.
func NewAIActStore(pool *pgxpool.Pool) *AIActStore { return &AIActStore{pool: pool} }

// LogOversight inserts a new oversight record. human_reviewed defaults to FALSE.
func (s *AIActStore) LogOversight(ctx context.Context, r AIActOversightRecord) error {
	var modelConfigID *string
	if r.ModelConfigID != "" {
		modelConfigID = &r.ModelConfigID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ai_act_oversight_log
		    (tenant_id, user_id, risk_level, risk_reasons, model_config_id, request_hash)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		r.TenantID, r.UserID, r.RiskLevel, r.RiskReasons, modelConfigID, r.RequestHash,
	)
	if err != nil {
		return fmt.Errorf("ai_act_store: log oversight: %w", err)
	}
	return nil
}

// MarkReviewed sets human_reviewed = TRUE for the given record ID.
func (s *AIActStore) MarkReviewed(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE ai_act_oversight_log SET human_reviewed = TRUE WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("ai_act_store: mark reviewed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ai_act_store: mark reviewed: record %s not found", id)
	}
	return nil
}
