-- Compression byte counters on usage_records for cost-savings reporting.
-- Both default to 0; non-zero values indicate the request was compressed.
ALTER TABLE usage_records
  ADD COLUMN IF NOT EXISTS prompt_bytes_original   INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS prompt_bytes_compressed INT NOT NULL DEFAULT 0;
