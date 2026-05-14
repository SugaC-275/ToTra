CREATE TABLE IF NOT EXISTS tenant_policy_rules (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    pattern     TEXT        NOT NULL,
    action      TEXT        NOT NULL DEFAULT 'block',
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_policy_rules_tenant_active
    ON tenant_policy_rules(tenant_id, is_active);
