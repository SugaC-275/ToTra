-- Add optional fallback model reference to model_configs.
-- When a primary model returns a 5xx error the gateway can retry with the fallback.
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS fallback_model_config_id UUID REFERENCES model_configs(id);
