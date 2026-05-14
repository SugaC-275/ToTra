CREATE TABLE IF NOT EXISTS gateway_routing_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    original_model TEXT NOT NULL,
    routed_model TEXT NOT NULL,
    body_len_bytes INT NOT NULL DEFAULT 0,
    routed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_routing_events_tenant_month ON gateway_routing_events(tenant_id, routed_at);
