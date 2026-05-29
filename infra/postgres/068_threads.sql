CREATE TABLE assistant_threads (
    thread_id   TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    session_id  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
