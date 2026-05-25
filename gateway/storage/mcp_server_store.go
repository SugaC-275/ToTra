package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MCPServerConfig holds the runtime configuration for a single MCP server.
type MCPServerConfig struct {
	ID           string
	Name         string
	URL          string
	AuthType     string
	AuthToken    string
	MaxToolCalls int
}

// MCPToolCallEntry is an audit record for a single MCP tool invocation.
type MCPToolCallEntry struct {
	TenantID     string
	UserID       string
	ServerID     string
	ServerName   string
	ToolName     string
	RequestArgs  json.RawMessage
	ResponseBody json.RawMessage
	StatusCode   int
	DurationMS   int
	PIIDetected  bool
}

// MCPServerStore reads MCP server configs and writes tool-call audit logs.
type MCPServerStore struct {
	pool *pgxpool.Pool
}

// NewMCPServerStore returns a store backed by the given connection pool.
func NewMCPServerStore(pool *pgxpool.Pool) *MCPServerStore {
	return &MCPServerStore{pool: pool}
}

// ListEnabled returns all enabled MCP servers for the given tenant.
func (s *MCPServerStore) ListEnabled(ctx context.Context, tenantID string) ([]MCPServerConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, url, auth_type, auth_token, max_tool_calls
		 FROM mcp_servers
		 WHERE tenant_id = $1 AND enabled = true
		 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("mcp_servers: query: %w", err)
	}
	defer rows.Close()

	var configs []MCPServerConfig
	for rows.Next() {
		var cfg MCPServerConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.Name, &cfg.URL,
			&cfg.AuthType, &cfg.AuthToken, &cfg.MaxToolCalls,
		); err != nil {
			return nil, fmt.Errorf("mcp_servers: scan: %w", err)
		}
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mcp_servers: iterate: %w", err)
	}
	return configs, nil
}

// LogToolCall writes a tool-call audit record. Errors are logged but not
// returned so callers can treat this as fire-and-forget.
func (s *MCPServerStore) LogToolCall(ctx context.Context, entry MCPToolCallEntry) error {
	reqArgs := entry.RequestArgs
	if len(reqArgs) == 0 {
		reqArgs = json.RawMessage("{}")
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO mcp_tool_calls
			(tenant_id, user_id, server_id, server_name, tool_name,
			 request_args, response_body, status_code, duration_ms, pii_detected)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		entry.TenantID,
		entry.UserID,
		entry.ServerID,
		entry.ServerName,
		entry.ToolName,
		reqArgs,
		entry.ResponseBody,
		entry.StatusCode,
		entry.DurationMS,
		entry.PIIDetected,
	)
	if err != nil {
		slog.Error("mcp: log tool call failed",
			"tenant", entry.TenantID,
			"server", entry.ServerName,
			"tool", entry.ToolName,
			"err", err,
		)
		return fmt.Errorf("mcp_tool_calls: insert: %w", err)
	}
	return nil
}
