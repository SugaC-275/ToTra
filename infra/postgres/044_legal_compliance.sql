-- Legal: privilege routing flag on model configs
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS attorney_privilege_routing BOOLEAN NOT NULL DEFAULT FALSE;

-- Privilege detection events (audit trail for legal hold)
CREATE TABLE IF NOT EXISTS privilege_detection_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    privilege_types TEXT[] NOT NULL,
    action_taken    TEXT NOT NULL DEFAULT 'flagged', -- 'flagged', 'blocked', 'routed'
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_privilege_events_tenant ON privilege_detection_events(tenant_id, detected_at DESC);
