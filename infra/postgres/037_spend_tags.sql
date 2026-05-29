-- Spend tagging: arbitrary user-supplied tags on usage records for cost aggregation.
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_usage_tags ON usage_records USING GIN(tags) WHERE array_length(tags, 1) > 0;
