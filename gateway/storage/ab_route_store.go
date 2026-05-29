package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// ABRouteRow is the full DB row for an A/B route (used for CRUD responses).
type ABRouteRow struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Name      string    `json:"name"`
	ModelA    string    `json:"model_a"`
	ModelB    string    `json:"model_b"`
	PctB      int       `json:"pct_b"`
	EvalSuite *string   `json:"eval_suite,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// ABRouteStore manages A/B routing rules in Postgres.
type ABRouteStore struct{ pool *pgxpool.Pool }

// NewABRouteStore creates an ABRouteStore backed by the given pool.
func NewABRouteStore(pool *pgxpool.Pool) *ABRouteStore {
	return &ABRouteStore{pool: pool}
}

// GetActiveRoute returns the active A/B route for a tenant where model_a matches modelName.
// Returns (nil, nil) when no active route is configured.
// Implements middleware.ABRouteQuerier.
func (s *ABRouteStore) GetActiveRoute(ctx context.Context, tenantID, modelName string) (*middleware.ABRoute, error) {
	var r middleware.ABRoute
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, model_a, model_b, pct_b, eval_suite
		 FROM ab_routes
		 WHERE tenant_id = $1 AND model_a = $2 AND is_active = true
		 LIMIT 1`,
		tenantID, modelName,
	).Scan(&r.ID, &r.Name, &r.ModelA, &r.ModelB, &r.PctB, &r.EvalSuite)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("ab_route_store get_active: %w", err)
	}
	return &r, nil
}

// List returns all A/B routes for a tenant.
func (s *ABRouteStore) List(ctx context.Context, tenantID string) ([]*ABRouteRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, model_a, model_b, pct_b, eval_suite, is_active, created_at
		 FROM ab_routes WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ab_route_store list: %w", err)
	}
	defer rows.Close()
	var routes []*ABRouteRow
	for rows.Next() {
		var r ABRouteRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.ModelA, &r.ModelB, &r.PctB,
			&r.EvalSuite, &r.IsActive, &r.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, &r)
	}
	return routes, rows.Err()
}

// Create inserts a new A/B route record and returns the persisted row.
func (s *ABRouteStore) Create(ctx context.Context, tenantID, name, modelA, modelB string, pctB int, evalSuite *string) (*ABRouteRow, error) {
	var r ABRouteRow
	err := s.pool.QueryRow(ctx,
		`INSERT INTO ab_routes (tenant_id, name, model_a, model_b, pct_b, eval_suite)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, tenant_id, name, model_a, model_b, pct_b, eval_suite, is_active, created_at`,
		tenantID, name, modelA, modelB, pctB, evalSuite,
	).Scan(&r.ID, &r.TenantID, &r.Name, &r.ModelA, &r.ModelB, &r.PctB,
		&r.EvalSuite, &r.IsActive, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ab_route_store create: %w", err)
	}
	return &r, nil
}

// Update replaces the mutable fields of an existing A/B route owned by tenantID.
func (s *ABRouteStore) Update(ctx context.Context, tenantID, id string, pctB int, evalSuite *string, isActive bool) (*ABRouteRow, error) {
	var r ABRouteRow
	err := s.pool.QueryRow(ctx,
		`UPDATE ab_routes
		 SET pct_b = $3, eval_suite = $4, is_active = $5
		 WHERE tenant_id = $1 AND id = $2
		 RETURNING id, tenant_id, name, model_a, model_b, pct_b, eval_suite, is_active, created_at`,
		tenantID, id, pctB, evalSuite, isActive,
	).Scan(&r.ID, &r.TenantID, &r.Name, &r.ModelA, &r.ModelB, &r.PctB,
		&r.EvalSuite, &r.IsActive, &r.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("ab_route_store update: %w", err)
	}
	return &r, nil
}

// Delete removes an A/B route owned by tenantID.
func (s *ABRouteStore) Delete(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM ab_routes WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	if err != nil {
		return fmt.Errorf("ab_route_store delete: %w", err)
	}
	return nil
}
