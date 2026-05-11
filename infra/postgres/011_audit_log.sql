BEGIN;

CREATE TABLE IF NOT EXISTS audit_log (
    id           SERIAL PRIMARY KEY,
    tenant_id    TEXT        NOT NULL,
    record_type  TEXT        NOT NULL,
    record_id    TEXT        NOT NULL,
    record_hash  TEXT        NOT NULL,
    prev_hash    TEXT        NOT NULL,
    chain_hash   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_tenant_id ON audit_log (tenant_id, id);

COMMIT;
