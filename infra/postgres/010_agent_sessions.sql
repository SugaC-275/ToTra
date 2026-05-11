-- Agent session tracking: one row per conversation_id
CREATE TABLE agent_sessions (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id  UUID        NOT NULL UNIQUE,
    loop_count       INTEGER     NOT NULL DEFAULT 0,
    tool_call_count  INTEGER     NOT NULL DEFAULT 0,
    is_dead_loop     BOOLEAN     NOT NULL DEFAULT FALSE,
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_sessions_tenant_user
    ON agent_sessions (tenant_id, user_id);

CREATE INDEX idx_agent_sessions_user
    ON agent_sessions (user_id, last_seen_at DESC);
