CREATE TABLE model_pricing_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_name      TEXT NOT NULL,
    provider        TEXT NOT NULL,
    input_per_mtok  NUMERIC(10,6) NOT NULL,  -- USD per million input tokens
    output_per_mtok NUMERIC(10,6) NOT NULL,
    effective_from  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source          TEXT NOT NULL DEFAULT 'manual'  -- 'manual' | 'auto_sync'
);
CREATE INDEX ON model_pricing_history(model_name, effective_from DESC);

-- View: current price per model
CREATE OR REPLACE VIEW current_model_pricing AS
SELECT DISTINCT ON (model_name) *
FROM model_pricing_history
ORDER BY model_name, effective_from DESC;

CREATE TABLE guardrail_configs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    strictness  TEXT NOT NULL DEFAULT 'standard',
    custom_config JSONB DEFAULT '{}',
    bundle_id   TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX ON guardrail_configs(tenant_id, name);
