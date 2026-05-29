package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	realtimeSessionTTL    = 24 * time.Hour
	realtimeSessionPrefix = "realtime:session:"
)

// RealtimeSession tracks an active or recently ended WebSocket realtime session
// for observability. Redis is the live store; Postgres persists after close.
type RealtimeSession struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id"`
	Model        string    `json:"model"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	PIIEvents    int       `json:"pii_events"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
}

// RealtimeSessionStore persists realtime session data in Redis (live) and
// Postgres (durable on close).
type RealtimeSessionStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
}

// NewRealtimeSessionStore creates a RealtimeSessionStore.
func NewRealtimeSessionStore(rdb *redis.Client, pool *pgxpool.Pool) *RealtimeSessionStore {
	return &RealtimeSessionStore{rdb: rdb, pool: pool}
}

func redisKey(sessionID string) string {
	return realtimeSessionPrefix + sessionID
}

// Create writes a new session to Redis with 24 h TTL.
func (s *RealtimeSessionStore) Create(ctx context.Context, sess *RealtimeSession) error {
	b, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("realtime session marshal: %w", err)
	}
	return s.rdb.Set(ctx, redisKey(sess.ID), b, realtimeSessionTTL).Err()
}

// IncrPIIEvents atomically adds 1 to the pii_events counter in Redis then
// re-serialises. Best-effort: errors are logged but not returned.
func (s *RealtimeSessionStore) IncrPIIEvents(ctx context.Context, sessionID string) {
	key := redisKey(sessionID)
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		log.Printf("realtime session incr pii: get %s: %v", sessionID, err)
		return
	}
	var sess RealtimeSession
	if err := json.Unmarshal(b, &sess); err != nil {
		log.Printf("realtime session incr pii: unmarshal %s: %v", sessionID, err)
		return
	}
	sess.PIIEvents++
	updated, _ := json.Marshal(&sess)
	// Preserve remaining TTL by using KEEPTTL (Redis 6+). Fall back to re-setting the full TTL.
	if err := s.rdb.Set(ctx, key, updated, realtimeSessionTTL).Err(); err != nil {
		log.Printf("realtime session incr pii: set %s: %v", sessionID, err)
	}
}

// Close marks the session ended, updates token/cost fields in Redis, and
// persists a final record to Postgres. Called from the hijack goroutine.
func (s *RealtimeSessionStore) Close(ctx context.Context, sessionID string, inputTokens, outputTokens int64, costUSD float64) {
	key := redisKey(sessionID)
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		log.Printf("realtime session close: get %s: %v", sessionID, err)
		s.persistToDB(ctx, &RealtimeSession{
			ID:           sessionID,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			CostUSD:      costUSD,
		})
		return
	}
	var sess RealtimeSession
	if err := json.Unmarshal(b, &sess); err != nil {
		log.Printf("realtime session close: unmarshal %s: %v", sessionID, err)
		return
	}
	now := time.Now().UTC()
	sess.EndedAt = &now
	sess.InputTokens = inputTokens
	sess.OutputTokens = outputTokens
	sess.CostUSD = costUSD

	// Update Redis entry with short residual TTL so recent sessions remain queryable.
	updated, _ := json.Marshal(&sess)
	_ = s.rdb.Set(ctx, key, updated, realtimeSessionTTL).Err()

	s.persistToDB(ctx, &sess)
}

func (s *RealtimeSessionStore) persistToDB(ctx context.Context, sess *RealtimeSession) {
	if s.pool == nil {
		return
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO realtime_sessions
		    (id, tenant_id, user_id, model, started_at, ended_at, pii_events, input_tokens, output_tokens, cost_usd)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (id) DO UPDATE SET
		    ended_at      = EXCLUDED.ended_at,
		    pii_events    = EXCLUDED.pii_events,
		    input_tokens  = EXCLUDED.input_tokens,
		    output_tokens = EXCLUDED.output_tokens,
		    cost_usd      = EXCLUDED.cost_usd`,
		sess.ID, sess.TenantID, sess.UserID, sess.Model, sess.StartedAt, sess.EndedAt,
		sess.PIIEvents, sess.InputTokens, sess.OutputTokens, sess.CostUSD,
	)
	if err != nil {
		log.Printf("realtime session persist: %v", err)
	}
}

// ListActive returns all sessions that have been stored in Redis (active and
// recently ended) for the given tenant. Scans the key prefix — suitable for
// low-volume admin queries only.
func (s *RealtimeSessionStore) ListActive(ctx context.Context, tenantID string) ([]*RealtimeSession, error) {
	pattern := realtimeSessionPrefix + "*"
	keys, err := s.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("realtime session list: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.Get(ctx, k)
	}
	_, _ = pipe.Exec(ctx)

	var sessions []*RealtimeSession
	for _, cmd := range cmds {
		b, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var sess RealtimeSession
		if err := json.Unmarshal(b, &sess); err != nil {
			continue
		}
		if tenantID != "" && sess.TenantID != tenantID {
			continue
		}
		sessions = append(sessions, &sess)
	}
	return sessions, nil
}
