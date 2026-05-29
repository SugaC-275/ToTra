package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThreadBinding maps an OpenAI thread_id to the ToTra tenant and session that
// owns it, enabling cost attribution for Assistants API usage.
type ThreadBinding struct {
	ThreadID  string
	TenantID  string
	SessionID string // empty when not bound to a session
	CreatedAt time.Time
}

// ThreadStore persists assistant_threads records.
type ThreadStore struct{ pool *pgxpool.Pool }

// NewThreadStore creates a ThreadStore backed by the given pool.
func NewThreadStore(pool *pgxpool.Pool) *ThreadStore {
	return &ThreadStore{pool: pool}
}

// BindThread associates an OpenAI thread_id with a tenant and optional session.
// A second call for the same thread_id is a no-op (ON CONFLICT DO NOTHING).
func (s *ThreadStore) BindThread(ctx context.Context, threadID, tenantID, sessionID string) error {
	var sessID *string
	if sessionID != "" {
		sessID = &sessionID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO assistant_threads (thread_id, tenant_id, session_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (thread_id) DO NOTHING`,
		threadID, tenantID, sessID,
	)
	if err != nil {
		return fmt.Errorf("bind thread: %w", err)
	}
	return nil
}

// GetThread returns the binding for a thread_id. Returns (nil, nil) when not found.
func (s *ThreadStore) GetThread(ctx context.Context, threadID string) (*ThreadBinding, error) {
	var b ThreadBinding
	var sessID *string
	err := s.pool.QueryRow(ctx,
		`SELECT thread_id, tenant_id, session_id, created_at
		 FROM assistant_threads WHERE thread_id = $1`,
		threadID,
	).Scan(&b.ThreadID, &b.TenantID, &sessID, &b.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if sessID != nil {
		b.SessionID = *sessID
	}
	return &b, nil
}
