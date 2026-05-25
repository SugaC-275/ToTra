-- MCP (Model Context Protocol) server registry.
-- Each tenant can register multiple MCP servers; the gateway uses them
-- to resolve tool calls in the /v1/mcp/chat agentic loop.

CREATE TABLE mcp_servers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL,
    name            TEXT        NOT NULL,                   -- unique per tenant, used as tool namespace
    description     TEXT        NOT NULL DEFAULT '',
    url             TEXT        NOT NULL,                   -- MCP server base URL (JSON-RPC 2.0 endpoint)
    auth_type       TEXT        NOT NULL DEFAULT 'none',    -- 'none' | 'bearer'
    auth_token      TEXT        NOT NULL DEFAULT '',        -- bearer token (stored encrypted via admin crypto)
    enabled         BOOLEAN     NOT NULL DEFAULT true,
    max_tool_calls  INT         NOT NULL DEFAULT 10,        -- max tool calls per /v1/mcp/chat request
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_mcp_servers_tenant_enabled ON mcp_servers(tenant_id) WHERE enabled = true;

-- mcp_tool_calls: immutable audit log of every tool call made through the gateway.
-- Provides the audit chain required for compliance.
CREATE TABLE mcp_tool_calls (
    id              BIGSERIAL   PRIMARY KEY,
    tenant_id       TEXT        NOT NULL,
    user_id         TEXT        NOT NULL,
    server_id       UUID        NOT NULL REFERENCES mcp_servers(id) ON DELETE SET NULL,
    server_name     TEXT        NOT NULL,
    tool_name       TEXT        NOT NULL,
    request_args    JSONB       NOT NULL DEFAULT '{}',
    response_body   JSONB,
    status_code     INT         NOT NULL,
    duration_ms     INT         NOT NULL DEFAULT 0,
    pii_detected    BOOLEAN     NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mcp_tool_calls_tenant ON mcp_tool_calls(tenant_id, created_at DESC);
CREATE INDEX idx_mcp_tool_calls_user   ON mcp_tool_calls(user_id,   created_at DESC);
