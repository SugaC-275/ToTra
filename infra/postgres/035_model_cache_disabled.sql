-- Per-model cache bypass flag.
-- When true, the gateway skips both exact and semantic cache for this model —
-- useful for real-time or time-sensitive models (news, stock prices, weather).
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS cache_disabled BOOLEAN NOT NULL DEFAULT false;
