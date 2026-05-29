CREATE TABLE llm_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    title           TEXT,               -- auto-set from first message (first 80 chars)
    model           TEXT,
    total_tokens    BIGINT NOT NULL DEFAULT 0,
    total_cost_usd  NUMERIC(12,6) NOT NULL DEFAULT 0,
    pii_events      INT NOT NULL DEFAULT 0,
    turn_count      INT NOT NULL DEFAULT 0,
    first_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ,
    is_active       BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX ON llm_sessions(tenant_id, user_id, last_at DESC);
CREATE INDEX ON llm_sessions(tenant_id, last_at DESC);

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS session_id UUID REFERENCES llm_sessions(id);
CREATE INDEX ON request_logs(session_id) WHERE session_id IS NOT NULL;
