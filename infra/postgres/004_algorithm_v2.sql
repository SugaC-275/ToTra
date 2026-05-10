-- quality signals on output events
ALTER TABLE output_events ADD COLUMN IF NOT EXISTS lines_changed   INTEGER   DEFAULT 0;
ALTER TABLE output_events ADD COLUMN IF NOT EXISTS cycle_time_hours FLOAT    DEFAULT 0;
ALTER TABLE output_events ADD COLUMN IF NOT EXISTS reopened_count  INTEGER   DEFAULT 0;

-- store component scores for explainability dashboard
ALTER TABLE efficiency_snapshots ADD COLUMN IF NOT EXISTS aiq_score        FLOAT DEFAULT 0;
ALTER TABLE efficiency_snapshots ADD COLUMN IF NOT EXISTS oss_score        FLOAT DEFAULT 0;
ALTER TABLE efficiency_snapshots ADD COLUMN IF NOT EXISTS gts_score        FLOAT DEFAULT 0;
ALTER TABLE efficiency_snapshots ADD COLUMN IF NOT EXISTS integration_level INTEGER DEFAULT 1;
