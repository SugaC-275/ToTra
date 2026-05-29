-- Financial compliance flags on model configs
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS finra_compliant BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sox_audit_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- PFI detection events for financial audit trail
CREATE TABLE IF NOT EXISTS pfi_detection_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    user_id     UUID NOT NULL REFERENCES users(id),
    pfi_types   TEXT[] NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pfi_events_tenant ON pfi_detection_events(tenant_id, detected_at DESC);
