CREATE TABLE IF NOT EXISTS tenant_routing_policy (
    tenant_id       TEXT PRIMARY KEY,
    w_complexity    NUMERIC(4,3) NOT NULL DEFAULT 0.4,
    w_latency       NUMERIC(4,3) NOT NULL DEFAULT 0.3,
    w_cost          NUMERIC(4,3) NOT NULL DEFAULT 0.3,
    sovereign_model TEXT,
    eu_only         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
