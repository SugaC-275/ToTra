CREATE TABLE request_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    model_config_id TEXT,
    provider TEXT,
    model TEXT,
    request_body JSONB,
    response_body JSONB,
    status_code INT,
    latency_ms INT,
    prompt_tokens INT,
    completion_tokens INT,
    cost_usd NUMERIC(12,8),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX request_logs_tenant_created ON request_logs(tenant_id, created_at DESC);
CREATE INDEX request_logs_user ON request_logs(user_id);
