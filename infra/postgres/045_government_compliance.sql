-- Government: FedRAMP and clearance level flags on model configs
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS fedramp_level TEXT CHECK (fedramp_level IN ('low','moderate','high','govcloud')) DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS clearance_level TEXT CHECK (clearance_level IN ('unclassified','cui','fouo','secret')) DEFAULT 'unclassified';

-- CUI (Controlled Unclassified Information) detection events
CREATE TABLE IF NOT EXISTS cui_detection_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    cui_categories TEXT[] NOT NULL,
    classification TEXT NOT NULL DEFAULT 'CUI',
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cui_events_tenant ON cui_detection_events(tenant_id, detected_at DESC);
