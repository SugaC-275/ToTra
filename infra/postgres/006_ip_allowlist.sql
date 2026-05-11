CREATE TABLE IF NOT EXISTS tenant_ip_allowlists (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cidr       TEXT        NOT NULL,
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, cidr)
);

CREATE INDEX IF NOT EXISTS idx_tenant_ip_allowlists_tenant_id
    ON tenant_ip_allowlists (tenant_id);
