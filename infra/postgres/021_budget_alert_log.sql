CREATE TABLE IF NOT EXISTS budget_alert_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    year_month  TEXT NOT NULL,
    threshold_pct INT NOT NULL,
    sent_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_budget_alert_log UNIQUE (tenant_id, year_month, threshold_pct)
);
CREATE INDEX IF NOT EXISTS idx_budget_alert_log_tenant ON budget_alert_log(tenant_id, year_month);
