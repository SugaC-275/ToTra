-- Virtual key model aliasing: per-user model name remapping without client changes.
ALTER TABLE users ADD COLUMN IF NOT EXISTS model_aliases JSONB NOT NULL DEFAULT '{}';
