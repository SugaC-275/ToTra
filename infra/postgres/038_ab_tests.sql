-- A/B testing: route a percentage of traffic to an alternate model.
CREATE TABLE IF NOT EXISTS ab_tests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    model_a       TEXT NOT NULL,
    model_b       TEXT NOT NULL,
    split_pct_b   INTEGER NOT NULL DEFAULT 50 CHECK (split_pct_b BETWEEN 1 AND 99),
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_ab_tests_tenant_active ON ab_tests(tenant_id, is_active);
