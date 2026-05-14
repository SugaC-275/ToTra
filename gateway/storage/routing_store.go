package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

type RoutingStore struct{ pool *pgxpool.Pool }

func NewRoutingStore(pool *pgxpool.Pool) *RoutingStore { return &RoutingStore{pool: pool} }

func (s *RoutingStore) Record(ctx context.Context, e middleware.RoutingEvent) {
	go func() {
		_, err := s.pool.Exec(context.Background(),
			`INSERT INTO gateway_routing_events (tenant_id, user_id, original_model, routed_model, body_len_bytes) VALUES ($1,$2,$3,$4,$5)`,
			e.TenantID, e.UserID, e.OriginalModel, e.RoutedModel, e.BodyLen)
		if err != nil {
			log.Printf("routing_store: record: %v", err)
		}
	}()
}
