-- EU AI Act: risk level flag on model configs
ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS eu_ai_act_risk TEXT
        CHECK (eu_ai_act_risk IN ('minimal','limited','high','unacceptable'))
        DEFAULT 'minimal';

-- Human oversight log: required for high-risk AI Act requests
CREATE TABLE IF NOT EXISTS ai_act_oversight_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    risk_level      TEXT NOT NULL,
    risk_reasons    TEXT[] NOT NULL,
    model_config_id TEXT,
    request_hash    TEXT NOT NULL,
    human_reviewed  BOOLEAN NOT NULL DEFAULT FALSE,
    logged_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ai_act_log_tenant ON ai_act_oversight_log(tenant_id, logged_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_act_log_unreviewed ON ai_act_oversight_log(human_reviewed, logged_at DESC) WHERE NOT human_reviewed;
