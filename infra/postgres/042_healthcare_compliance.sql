-- Healthcare: BAA compliance flag on model configs
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS baa_compliant BOOLEAN NOT NULL DEFAULT FALSE;

-- PHI detection events (separate from general PII for HIPAA audit)
CREATE TABLE IF NOT EXISTS phi_detection_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    user_id     UUID NOT NULL REFERENCES users(id),
    phi_types   TEXT[] NOT NULL,
    routed_to   TEXT,
    blocked     BOOLEAN NOT NULL DEFAULT FALSE,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_phi_events_tenant ON phi_detection_events(tenant_id, detected_at DESC);
