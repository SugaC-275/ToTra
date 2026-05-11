BEGIN;

CREATE TABLE IF NOT EXISTS tenant_bot_configs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    platform        TEXT        NOT NULL CHECK (platform IN ('feishu', 'slack')),
    encrypted_url   TEXT        NOT NULL,
    label           TEXT        NOT NULL DEFAULT '',
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform, label)
);

CREATE INDEX IF NOT EXISTS idx_tenant_bot_configs_tenant_id
    ON tenant_bot_configs (tenant_id);

COMMIT;
