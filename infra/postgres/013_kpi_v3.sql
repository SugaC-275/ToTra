ALTER TABLE efficiency_snapshots
    ADD COLUMN IF NOT EXISTS anomaly_soft_flagged BOOLEAN NOT NULL DEFAULT FALSE;
