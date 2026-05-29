CREATE TABLE IF NOT EXISTS provider_api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_config_id TEXT NOT NULL REFERENCES model_configs(id) ON DELETE CASCADE,
    api_key_encrypted TEXT NOT NULL,
    weight          INTEGER NOT NULL DEFAULT 1,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at    TIMESTAMPTZ,
    error_count     INTEGER NOT NULL DEFAULT 0,
    cooldown_until  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pak_model_config ON provider_api_keys(model_config_id, is_active);
