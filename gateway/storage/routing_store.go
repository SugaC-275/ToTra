package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// ModelPrice holds per-million-token pricing for a model.
type ModelPrice struct {
	PricePerMInput  float64
	PricePerMOutput float64
}

type RoutingStore struct{ pool *pgxpool.Pool }

func NewRoutingStore(pool *pgxpool.Pool) *RoutingStore { return &RoutingStore{pool: pool} }

// Record inserts a routing event synchronously and returns the new row ID.
// Synchronous insertion is required so the ID can be stored in c.Locals for
// later use by UpdateTokensAndSavings.
func (s *RoutingStore) Record(ctx context.Context, e middleware.RoutingEvent) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO gateway_routing_events
		 (tenant_id, user_id, original_model, routed_model, body_len_bytes, complexity_score)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		e.TenantID, e.UserID, e.OriginalModel, e.RoutedModel, e.BodyLen, e.ComplexityScore,
	).Scan(&id)
	if err != nil {
		log.Printf("routing_store: record: %v", err)
		return 0, err
	}
	return id, nil
}

// UpdateTokensAndSavings back-fills token counts and USD savings onto an existing
// routing event row after the upstream response has been received.
// Pass nil for orig or routed if either model has no pricing configured —
// usd_saved will be left NULL.
func (s *RoutingStore) UpdateTokensAndSavings(ctx context.Context, id int64, promptTokens, completionTokens int, orig, routed *ModelPrice) error {
	var usdSaved *float64
	if orig != nil && routed != nil {
		saved := float64(promptTokens)/1_000_000*(orig.PricePerMInput-routed.PricePerMInput) +
			float64(completionTokens)/1_000_000*(orig.PricePerMOutput-routed.PricePerMOutput)
		usdSaved = &saved
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE gateway_routing_events
		 SET prompt_tokens=$1, completion_tokens=$2, usd_saved=$3
		 WHERE id=$4`,
		promptTokens, completionTokens, usdSaved, id)
	if err != nil {
		log.Printf("routing_store: update tokens/savings id=%d: %v", id, err)
	}
	return err
}
