-- Canonical model pricing table for auto-sync from LiteLLM pricing database.
CREATE TABLE IF NOT EXISTS canonical_model_pricing (
    model_name           TEXT PRIMARY KEY,
    input_cost_per_token  FLOAT,
    output_cost_per_token FLOAT,
    max_tokens           INTEGER,
    source               TEXT NOT NULL DEFAULT 'litellm',
    synced_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
