-- Pharma: FDA 21 CFR Part 11 audit flag
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS fda_21cfr_audit BOOLEAN NOT NULL DEFAULT FALSE;

-- Insurance: state compliance flag
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS insurance_compliant BOOLEAN NOT NULL DEFAULT FALSE;

-- FDA regulated data detection events
CREATE TABLE IF NOT EXISTS fda_data_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    data_types    TEXT[] NOT NULL,
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insurance sensitive data events
CREATE TABLE IF NOT EXISTS insurance_data_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    data_types    TEXT[] NOT NULL,
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
