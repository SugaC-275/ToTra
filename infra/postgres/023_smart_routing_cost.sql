-- Per-model pricing for USD savings calculation (NULL = no pricing configured)
ALTER TABLE model_configs
  ADD COLUMN IF NOT EXISTS price_per_m_input  NUMERIC(10,4),
  ADD COLUMN IF NOT EXISTS price_per_m_output NUMERIC(10,4);

-- Extended routing event fields backfilled after the upstream response
ALTER TABLE gateway_routing_events
  ADD COLUMN IF NOT EXISTS complexity_score   INT,
  ADD COLUMN IF NOT EXISTS prompt_tokens      INT,
  ADD COLUMN IF NOT EXISTS completion_tokens  INT,
  ADD COLUMN IF NOT EXISTS usd_saved          NUMERIC(12,6);
