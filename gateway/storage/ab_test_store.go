package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

type ABTestStore struct{ pool *pgxpool.Pool }

func NewABTestStore(pool *pgxpool.Pool) *ABTestStore { return &ABTestStore{pool: pool} }

// GetActiveForModel returns an active A/B test for the given tenant and model,
// where model matches either model_a or model_b.
// Returns (nil, nil) when no active test is configured.
func (s *ABTestStore) GetActiveForModel(ctx context.Context, tenantID, model string) (*middleware.ABTestEntry, error) {
	var t middleware.ABTestEntry
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, model_a, model_b, split_pct_b
		 FROM ab_tests
		 WHERE tenant_id = $1 AND is_active = true AND (model_a = $2 OR model_b = $2)
		 LIMIT 1`,
		tenantID, model,
	).Scan(&t.ID, &t.Name, &t.ModelA, &t.ModelB, &t.SplitPctB)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("ab_test_store: get active: %w", err)
	}
	return &t, nil
}
