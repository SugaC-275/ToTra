-- Add password_hash to users for proper authentication (TD-1 fix)
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT;

-- Dev seed: update existing seed users with a known bcrypt hash.
-- Run scripts/set-dev-passwords/main.go to regenerate after changing passwords.
