package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentSession is the DTO returned by AgentService methods.
type AgentSession struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	ConversationID string `json:"conversation_id"`
	LoopCount      int    `json:"loop_count"`
	ToolCallCount  int    `json:"tool_call_count"`
	IsDeadLoop     bool   `json:"is_dead_loop"`
	LastSeenAt     string `json:"last_seen_at"`
	CreatedAt      string `json:"created_at"`
}

// AgentService reads agent_sessions from Postgres.
type AgentService struct {
	db dbQuerier
}

// NewAgentService accepts pgxmock.PgxPoolIface (tests) or *pgxpool.Pool wrapped via agentPoolAdapter.
func NewAgentService(db dbQuerier) *AgentService {
	return &AgentService{db: db}
}

// NewAgentServiceFromPool is the production constructor.
func NewAgentServiceFromPool(pool *pgxpool.Pool) *AgentService {
	return &AgentService{db: pool}
}

const agentSessionSelectCols = `
	a.id, a.tenant_id, a.user_id, u.name AS user_name,
	a.conversation_id::text, a.loop_count, a.tool_call_count,
	a.is_dead_loop, a.last_seen_at, a.created_at`

func (s *AgentService) GetAgentSessions(ctx context.Context, tenantID, yearMonth string) ([]*AgentSession, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+agentSessionSelectCols+`
		FROM agent_sessions a
		JOIN users u ON u.id = a.user_id
		WHERE a.tenant_id = $1
		  AND to_char(a.last_seen_at, 'YYYY-MM') = $2
		ORDER BY a.last_seen_at DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*AgentSession
	for rows.Next() {
		var s AgentSession
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.UserID, &s.UserName,
			&s.ConversationID, &s.LoopCount, &s.ToolCallCount,
			&s.IsDeadLoop, &s.LastSeenAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (s *AgentService) GetMyAgentSessions(ctx context.Context, userID, yearMonth string) ([]*AgentSession, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+agentSessionSelectCols+`
		FROM agent_sessions a
		JOIN users u ON u.id = a.user_id
		WHERE a.user_id = $1
		  AND to_char(a.last_seen_at, 'YYYY-MM') = $2
		ORDER BY a.last_seen_at DESC`,
		userID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*AgentSession
	for rows.Next() {
		var s AgentSession
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.UserID, &s.UserName,
			&s.ConversationID, &s.LoopCount, &s.ToolCallCount,
			&s.IsDeadLoop, &s.LastSeenAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}
