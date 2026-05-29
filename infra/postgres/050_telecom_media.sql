-- Telecom: CPNI compliance flag
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS cpni_compliant BOOLEAN NOT NULL DEFAULT FALSE;

-- CPNI detection events (FCC-mandated logging)
CREATE TABLE IF NOT EXISTS cpni_detection_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    cpni_types    TEXT[] NOT NULL,
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Copyright/DMCA detection events
CREATE TABLE IF NOT EXISTS copyright_detection_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    signal_types    TEXT[] NOT NULL,
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
