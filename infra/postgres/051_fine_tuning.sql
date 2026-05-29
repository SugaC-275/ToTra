CREATE TABLE IF NOT EXISTS fine_tuning_jobs (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    model_config_id TEXT NOT NULL,
    base_model      TEXT NOT NULL,
    training_file   TEXT NOT NULL,
    validation_file TEXT,
    status          TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued','validating_files','running','succeeded','failed','cancelled')),
    fine_tuned_model TEXT,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ft_jobs_tenant  ON fine_tuning_jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ft_jobs_running ON fine_tuning_jobs(status)
    WHERE status IN ('queued','running','validating_files');

CREATE TABLE IF NOT EXISTS fine_tuning_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id     TEXT NOT NULL REFERENCES fine_tuning_jobs(id) ON DELETE CASCADE,
    level      TEXT NOT NULL DEFAULT 'info',
    message    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
