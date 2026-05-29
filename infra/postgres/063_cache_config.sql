CREATE TABLE IF NOT EXISTS cache_configs (
    tenant_id            TEXT PRIMARY KEY,
    exact_ttl_seconds    INT      NOT NULL DEFAULT 3600,
    semantic_ttl_seconds INT      NOT NULL DEFAULT 7200,
    semantic_enabled     BOOLEAN  NOT NULL DEFAULT true,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
