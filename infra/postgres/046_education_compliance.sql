-- Education: COPPA and content rating flags
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS coppa_compliant BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS content_rating TEXT CHECK (content_rating IN ('G','PG','PG-13','R','unrated')) DEFAULT 'unrated';

-- FERPA detection events
CREATE TABLE IF NOT EXISTS ferpa_detection_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    ferpa_types   TEXT[] NOT NULL,
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ferpa_events_tenant ON ferpa_detection_events(tenant_id, detected_at DESC);
