ALTER TABLE model_configs
    ADD COLUMN IF NOT EXISTS hipaa_eligible   BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS govcloud         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS fedramp_auth     BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS data_region      TEXT,   -- 'us', 'eu', 'us-gov', 'au', etc.
    ADD COLUMN IF NOT EXISTS compliance_notes TEXT;
