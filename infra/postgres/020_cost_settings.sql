CREATE TABLE IF NOT EXISTS tenant_cost_settings (
    id                 BIGSERIAL   PRIMARY KEY,
    tenant_id          TEXT        NOT NULL UNIQUE,
    monthly_budget_usd NUMERIC(12,2),
    work_start_hour    INT         NOT NULL DEFAULT 9,
    work_end_hour      INT         NOT NULL DEFAULT 18,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cost_settings_tenant ON tenant_cost_settings(tenant_id);
