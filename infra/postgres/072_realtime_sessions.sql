-- 072_realtime_sessions.sql
-- Durable record of WebSocket realtime sessions with PII event count and token usage.

CREATE TABLE IF NOT EXISTS realtime_sessions (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT,
    model           TEXT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at        TIMESTAMPTZ,
    pii_events      INT NOT NULL DEFAULT 0,
    input_tokens    BIGINT NOT NULL DEFAULT 0,
    output_tokens   BIGINT NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(12,6) NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS realtime_sessions_tenant_started
    ON realtime_sessions(tenant_id, started_at DESC);
