-- Per-tenant request-per-minute rate limit configuration.
-- Tenant-level and per-user-per-minute ceilings are stored here;
-- the sliding window counters themselves live in Redis.
CREATE TABLE IF NOT EXISTS rate_limit_configs (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       TEXT NOT NULL,
    max_requests_per_minute         INT  NOT NULL DEFAULT 60,
    max_requests_per_minute_per_user INT NOT NULL DEFAULT 20,
    created_at                      TIMESTAMPTZ DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(tenant_id)
);
