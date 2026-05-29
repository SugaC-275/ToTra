-- HR: EEOC compliance flag
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS eeoc_bias_check BOOLEAN NOT NULL DEFAULT FALSE;

-- EEOC bias detection events
CREATE TABLE IF NOT EXISTS eeoc_bias_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    bias_types    TEXT[] NOT NULL,
    action        TEXT NOT NULL DEFAULT 'flagged',
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fair Housing violation signals
CREATE TABLE IF NOT EXISTS fair_housing_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    violation_types TEXT[] NOT NULL,
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
