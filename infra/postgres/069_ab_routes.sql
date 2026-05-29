CREATE TABLE ab_routes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    model_a     TEXT NOT NULL,
    model_b     TEXT NOT NULL,
    pct_b       INT NOT NULL DEFAULT 50 CHECK (pct_b BETWEEN 1 AND 99),
    eval_suite  TEXT,           -- if set, auto-submit both responses to this eval suite
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX ON ab_routes(tenant_id, name) WHERE is_active = true;
