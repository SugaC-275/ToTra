CREATE TABLE IF NOT EXISTS uploaded_files (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    model_config_id TEXT NOT NULL,
    filename        TEXT NOT NULL,
    purpose         TEXT NOT NULL CHECK (purpose IN ('fine-tune','assistants','vision','batch')),
    bytes           BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'uploaded',
    provider        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_files_tenant ON uploaded_files(tenant_id, created_at DESC);
