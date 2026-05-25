package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

// MCPServer represents an MCP server configuration (auth_token never returned).
type MCPServer struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	URL          string    `json:"url"`
	AuthType     string    `json:"auth_type"`
	Enabled      bool      `json:"enabled"`
	MaxToolCalls int       `json:"max_tool_calls"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateMCPServerRequest is the input for creating an MCP server.
type CreateMCPServerRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	AuthType     string `json:"auth_type"`
	AuthToken    string `json:"auth_token"`
	MaxToolCalls int    `json:"max_tool_calls"`
}

// UpdateMCPServerRequest supports partial updates; nil fields are not changed.
type UpdateMCPServerRequest struct {
	Description  *string `json:"description"`
	URL          *string `json:"url"`
	AuthType     *string `json:"auth_type"`
	AuthToken    *string `json:"auth_token"`
	Enabled      *bool   `json:"enabled"`
	MaxToolCalls *int    `json:"max_tool_calls"`
}

// MCPToolCallLog is a single audit record from mcp_tool_calls.
type MCPToolCallLog struct {
	ID          int64     `json:"id"`
	ServerName  string    `json:"server_name"`
	ToolName    string    `json:"tool_name"`
	StatusCode  int       `json:"status_code"`
	DurationMS  int       `json:"duration_ms"`
	PIIDetected bool      `json:"pii_detected"`
	CreatedAt   time.Time `json:"created_at"`
}

// MCPServerService handles CRUD for mcp_servers and audit log queries.
type MCPServerService struct {
	pool   *pgxpool.Pool
	encKey string
}

// NewMCPServerService creates a new MCPServerService.
func NewMCPServerService(pool *pgxpool.Pool, encKey string) *MCPServerService {
	return &MCPServerService{pool: pool, encKey: encKey}
}

// List returns all MCP servers for a tenant (never nil slice).
func (s *MCPServerService) List(ctx context.Context, tenantID string) ([]*MCPServer, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, description, url, auth_type,
		        enabled, max_tool_calls, created_at, updated_at
		 FROM mcp_servers
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := []*MCPServer{}
	for rows.Next() {
		m := &MCPServer{}
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Description, &m.URL, &m.AuthType,
			&m.Enabled, &m.MaxToolCalls, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		servers = append(servers, m)
	}
	return servers, rows.Err()
}

// Get returns a single MCP server by id scoped to the tenant.
func (s *MCPServerService) Get(ctx context.Context, tenantID, id string) (*MCPServer, error) {
	m := &MCPServer{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, description, url, auth_type,
		        enabled, max_tool_calls, created_at, updated_at
		 FROM mcp_servers
		 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Description, &m.URL, &m.AuthType,
		&m.Enabled, &m.MaxToolCalls, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("mcp server not found: %w", err)
	}
	return m, nil
}

// Create inserts a new MCP server; auth_token is encrypted at rest.
func (s *MCPServerService) Create(ctx context.Context, tenantID string, req CreateMCPServerRequest) (*MCPServer, error) {
	maxCalls := req.MaxToolCalls
	if maxCalls <= 0 {
		maxCalls = 10
	}

	encToken := ""
	if req.AuthToken != "" {
		var err error
		encToken, err = crypto.Encrypt(req.AuthToken, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt auth_token: %w", err)
		}
	}

	m := &MCPServer{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO mcp_servers
		   (tenant_id, name, description, url, auth_type, auth_token, enabled, max_tool_calls)
		 VALUES ($1, $2, $3, $4, $5, $6, true, $7)
		 RETURNING id, tenant_id, name, description, url, auth_type,
		           enabled, max_tool_calls, created_at, updated_at`,
		tenantID, req.Name, req.Description, req.URL,
		req.AuthType, encToken, maxCalls,
	).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Description, &m.URL, &m.AuthType,
		&m.Enabled, &m.MaxToolCalls, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert mcp_server: %w", err)
	}
	return m, nil
}

// Update applies partial updates to an MCP server.
func (s *MCPServerService) Update(ctx context.Context, tenantID, id string, req UpdateMCPServerRequest) (*MCPServer, error) {
	// Build SET clause dynamically.
	setClauses := []string{"updated_at = NOW()"}
	args := []any{tenantID, id}
	argIdx := 3 // $1=tenantID, $2=id

	addArg := func(clause string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", clause, argIdx))
		args = append(args, val)
		argIdx++
	}

	if req.Description != nil {
		addArg("description", *req.Description)
	}
	if req.URL != nil {
		addArg("url", *req.URL)
	}
	if req.AuthType != nil {
		addArg("auth_type", *req.AuthType)
	}
	if req.AuthToken != nil {
		encToken, err := crypto.Encrypt(*req.AuthToken, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt auth_token: %w", err)
		}
		addArg("auth_token", encToken)
	}
	if req.Enabled != nil {
		addArg("enabled", *req.Enabled)
	}
	if req.MaxToolCalls != nil {
		addArg("max_tool_calls", *req.MaxToolCalls)
	}

	// Build the full query.
	setSQL := ""
	for i, clause := range setClauses {
		if i > 0 {
			setSQL += ", "
		}
		setSQL += clause
	}

	query := fmt.Sprintf(
		`UPDATE mcp_servers SET %s
		 WHERE tenant_id = $1 AND id = $2
		 RETURNING id, tenant_id, name, description, url, auth_type,
		           enabled, max_tool_calls, created_at, updated_at`,
		setSQL,
	)

	m := &MCPServer{}
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Description, &m.URL, &m.AuthType,
		&m.Enabled, &m.MaxToolCalls, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update mcp_server: %w", err)
	}
	return m, nil
}

// Delete removes an MCP server; returns an error if not found.
func (s *MCPServerService) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mcp_servers WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mcp server not found: %s", id)
	}
	return nil
}

// ListToolCalls returns paginated tool-call audit logs for a tenant.
// limit is capped at 100; if <= 0 it defaults to 50.
func (s *MCPServerService) ListToolCalls(ctx context.Context, tenantID string, limit, offset int) ([]*MCPToolCallLog, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	var total int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mcp_tool_calls WHERE tenant_id = $1`,
		tenantID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, server_name, tool_name, status_code, duration_ms, pii_detected, created_at
		 FROM mcp_tool_calls
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		tenantID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	logs := []*MCPToolCallLog{}
	for rows.Next() {
		r := &MCPToolCallLog{}
		if err := rows.Scan(
			&r.ID, &r.ServerName, &r.ToolName,
			&r.StatusCode, &r.DurationMS, &r.PIIDetected, &r.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		logs = append(logs, r)
	}
	return logs, total, rows.Err()
}
