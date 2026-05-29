ALTER TABLE users
    ADD COLUMN IF NOT EXISTS budget_usd      NUMERIC(12,4),
    ADD COLUMN IF NOT EXISTS budget_period   TEXT DEFAULT 'monthly'
        CHECK (budget_period IN ('daily','monthly','total')),
    ADD COLUMN IF NOT EXISTS rpm_limit       INTEGER,
    ADD COLUMN IF NOT EXISTS tpm_limit       INTEGER;

CREATE TABLE IF NOT EXISTS budget_consumption_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    usd_spent   NUMERIC(12,4) NOT NULL DEFAULT 0,
    period_key  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, period_key)
);
CREATE INDEX IF NOT EXISTS idx_budget_consumption_user ON budget_consumption_log(user_id, period_key);
