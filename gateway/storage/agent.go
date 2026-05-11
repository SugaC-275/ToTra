package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const agentLoopTTL = time.Hour

// AgentRecord describes one completed agent-mode request to be persisted.
type AgentRecord struct {
	TenantID       string
	UserID         string
	ConversationID string
	ToolCallCount  int
	IsDeadLoop     bool
}

// AgentStore handles Redis loop counting and Postgres upsert for agent sessions.
type AgentStore struct {
	rdb       *redis.Client
	pool      *pgxpool.Pool
	loopLimit int64
	ch        chan *AgentRecord
}

// NewAgentStore creates an AgentStore. pool may be nil in unit tests.
func NewAgentStore(rdb *redis.Client, pool *pgxpool.Pool, loopLimit int64) *AgentStore {
	s := &AgentStore{
		rdb:       rdb,
		pool:      pool,
		loopLimit: loopLimit,
		ch:        make(chan *AgentRecord, 1000),
	}
	go s.runWorker()
	return s
}

// IncrLoop atomically increments the per-conversation loop counter in Redis
// (TTL = 1 hour) and returns (newCount, exceeded, error).
func (s *AgentStore) IncrLoop(ctx context.Context, conversationID string) (int64, bool, error) {
	key := fmt.Sprintf("agent:loops:%s", conversationID)
	count, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, false, fmt.Errorf("agent loop incr: %w", err)
	}
	_ = s.rdb.Expire(ctx, key, agentLoopTTL).Err()
	return count, count > s.loopLimit, nil
}

// Record enqueues an agent session upsert for async write. Never blocks.
func (s *AgentStore) Record(r *AgentRecord) {
	select {
	case s.ch <- r:
	default:
		log.Println("agent record channel full, dropping record")
	}
}

func (s *AgentStore) runWorker() {
	for r := range s.ch {
		if s.pool == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.upsert(ctx, r); err != nil {
			log.Printf("failed to upsert agent session: %v", err)
		}
		cancel()
	}
}

func (s *AgentStore) upsert(ctx context.Context, r *AgentRecord) error {
	convID, err := uuid.Parse(r.ConversationID)
	if err != nil {
		return nil // non-UUID conversation_id: skip silently
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO agent_sessions
			(tenant_id, user_id, conversation_id, loop_count, tool_call_count, is_dead_loop, last_seen_at)
		VALUES ($1, $2, $3, 1, $4, $5, NOW())
		ON CONFLICT (conversation_id) DO UPDATE
			SET loop_count      = agent_sessions.loop_count + 1,
			    tool_call_count = agent_sessions.tool_call_count + EXCLUDED.tool_call_count,
			    is_dead_loop    = EXCLUDED.is_dead_loop,
			    last_seen_at    = NOW()`,
		r.TenantID, r.UserID, convID, r.ToolCallCount, r.IsDeadLoop,
	)
	if err != nil {
		return fmt.Errorf("upsert agent session: %w", err)
	}
	return nil
}
