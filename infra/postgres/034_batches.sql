-- Batch job queue for async LLM inference (OpenAI-compatible /v1/batches API).
-- Status flow: queued → processing → completed | failed | cancelled

CREATE TABLE batch_jobs (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT        NOT NULL,
    user_id             TEXT        NOT NULL,
    model_name          TEXT        NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'queued',
    total_requests      INT         NOT NULL DEFAULT 0,
    completed_requests  INT         NOT NULL DEFAULT 0,
    failed_requests     INT         NOT NULL DEFAULT 0,
    metadata            JSONB       NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    CONSTRAINT chk_batch_status CHECK (status IN ('queued','processing','completed','failed','cancelled'))
);

CREATE INDEX idx_batch_jobs_tenant    ON batch_jobs(tenant_id, created_at DESC);
CREATE INDEX idx_batch_jobs_pending   ON batch_jobs(status, created_at) WHERE status IN ('queued','processing');

CREATE TABLE batch_requests (
    id              BIGSERIAL   PRIMARY KEY,
    job_id          UUID        NOT NULL REFERENCES batch_jobs(id) ON DELETE CASCADE,
    custom_id       TEXT        NOT NULL,
    request_body    JSONB       NOT NULL,
    response_body   JSONB,
    status          TEXT        NOT NULL DEFAULT 'pending',
    error_message   TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ,
    UNIQUE(job_id, custom_id),
    CONSTRAINT chk_batch_req_status CHECK (status IN ('pending','completed','failed'))
);

CREATE INDEX idx_batch_requests_job_pending ON batch_requests(job_id, status) WHERE status = 'pending';
