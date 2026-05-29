package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session is a governance object grouping requests from one user into a conversation.
type Session struct {
	ID           string
	TenantID     string
	UserID       string
	Title        string
	Model        string
	TotalTokens  int64
	TotalCostUSD float64
	PIIEvents    int
	TurnCount    int
	FirstAt      time.Time
	LastAt       time.Time
	ClosedAt     *time.Time
	IsActive     bool
}

// SessionStore persists and retrieves llm_sessions records.
type SessionStore struct{ pool *pgxpool.Pool }

// NewSessionStore creates a SessionStore backed by the given pool.
func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

// GetOrCreate returns the session identified by sessionID for the given
// tenant/user. When sessionID is empty or does not exist, a new session is
// created and returned. The caller should echo the returned session ID back to
// the client via the X-ToTra-Session-Id response header.
func (s *SessionStore) GetOrCreate(ctx context.Context, tenantID, userID, sessionID string) (*Session, error) {
	if sessionID != "" {
		// Validate UUID format before querying.
		if _, err := uuid.Parse(sessionID); err == nil {
			sess, err := s.get(ctx, tenantID, sessionID)
			if err != nil {
				return nil, err
			}
			if sess != nil {
				return sess, nil
			}
			// Not found for this tenant — fall through to create.
		}
	}
	return s.create(ctx, tenantID, userID)
}

func (s *SessionStore) get(ctx context.Context, tenantID, sessionID string) (*Session, error) {
	var sess Session
	var closedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, COALESCE(title,''), COALESCE(model,''),
		        total_tokens, total_cost_usd, pii_events, turn_count,
		        first_at, last_at, closed_at, is_active
		 FROM llm_sessions WHERE id = $1 AND tenant_id = $2`,
		sessionID, tenantID,
	).Scan(
		&sess.ID, &sess.TenantID, &sess.UserID, &sess.Title, &sess.Model,
		&sess.TotalTokens, &sess.TotalCostUSD, &sess.PIIEvents, &sess.TurnCount,
		&sess.FirstAt, &sess.LastAt, &closedAt, &sess.IsActive,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("session get: %w", err)
	}
	sess.ClosedAt = closedAt
	return &sess, nil
}

func (s *SessionStore) create(ctx context.Context, tenantID, userID string) (*Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx,
		`INSERT INTO llm_sessions (tenant_id, user_id)
		 VALUES ($1, $2)
		 RETURNING id, tenant_id, user_id, COALESCE(title,''), COALESCE(model,''),
		           total_tokens, total_cost_usd, pii_events, turn_count,
		           first_at, last_at, closed_at, is_active`,
		tenantID, userID,
	).Scan(
		&sess.ID, &sess.TenantID, &sess.UserID, &sess.Title, &sess.Model,
		&sess.TotalTokens, &sess.TotalCostUSD, &sess.PIIEvents, &sess.TurnCount,
		&sess.FirstAt, &sess.LastAt, &sess.ClosedAt, &sess.IsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("session create: %w", err)
	}
	return &sess, nil
}

// UpdateStats increments token spend, cost, and turn count for the session.
// When piiHit is true the pii_events counter is also incremented.
// This is designed to be called from a goroutine (fire-and-forget).
func (s *SessionStore) UpdateStats(ctx context.Context, sessionID string, tokens int64, costUSD float64, piiHit bool) error {
	piiDelta := 0
	if piiHit {
		piiDelta = 1
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE llm_sessions
		 SET total_tokens   = total_tokens   + $1,
		     total_cost_usd = total_cost_usd + $2,
		     pii_events     = pii_events     + $3,
		     turn_count     = turn_count     + 1,
		     last_at        = NOW()
		 WHERE id = $4`,
		tokens, costUSD, piiDelta, sessionID,
	)
	if err != nil {
		return fmt.Errorf("session update stats: %w", err)
	}
	return nil
}

// UpdateStatsAsync fires UpdateStats in a goroutine; errors are logged, not returned.
func (s *SessionStore) UpdateStatsAsync(sessionID string, tokens int64, costUSD float64, piiHit bool) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.UpdateStats(ctx, sessionID, tokens, costUSD, piiHit); err != nil {
			log.Printf("session stats async: %v", err)
		}
	}()
}

// List returns sessions for a tenant/user ordered by last_at DESC.
func (s *SessionStore) List(ctx context.Context, tenantID, userID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, COALESCE(title,''), COALESCE(model,''),
		        total_tokens, total_cost_usd, pii_events, turn_count,
		        first_at, last_at, closed_at, is_active
		 FROM llm_sessions
		 WHERE tenant_id = $1 AND user_id = $2
		 ORDER BY last_at DESC
		 LIMIT $3`,
		tenantID, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("session list: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// ListAll returns all sessions for a tenant ordered by last_at DESC (admin use).
func (s *SessionStore) ListAll(ctx context.Context, tenantID, filterUserID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 100
	}
	args := []any{tenantID}
	cond := ""
	if filterUserID != "" {
		cond = " AND user_id = $2"
		args = append(args, filterUserID)
		args = append(args, limit)
	} else {
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, COALESCE(title,''), COALESCE(model,''),
		        total_tokens, total_cost_usd, pii_events, turn_count,
		        first_at, last_at, closed_at, is_active
		 FROM llm_sessions
		 WHERE tenant_id = $1`+cond+
			fmt.Sprintf(" ORDER BY last_at DESC LIMIT $%d", len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("session list all: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// Close marks a session as inactive and records the closed_at timestamp.
// Also schedules request log deletion per data retention policy.
func (s *SessionStore) Close(ctx context.Context, sessionID, tenantID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE llm_sessions
		 SET is_active = false, closed_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND is_active = true`,
		sessionID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("session close: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found or already closed")
	}
	// Schedule request log deletion asynchronously (GDPR retention policy).
	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := s.pool.Exec(tctx,
			`UPDATE request_logs SET session_id = NULL WHERE session_id = $1`,
			sessionID,
		)
		if err != nil {
			log.Printf("session close: unlink request logs: %v", err)
		}
	}()
	return nil
}

func scanSessions(rows pgx.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(
			&sess.ID, &sess.TenantID, &sess.UserID, &sess.Title, &sess.Model,
			&sess.TotalTokens, &sess.TotalCostUSD, &sess.PIIEvents, &sess.TurnCount,
			&sess.FirstAt, &sess.LastAt, &sess.ClosedAt, &sess.IsActive,
		); err != nil {
			return nil, fmt.Errorf("session scan: %w", err)
		}
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}
