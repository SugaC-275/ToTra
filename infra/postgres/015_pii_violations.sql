CREATE TABLE pii_violations (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   UUID         NOT NULL,
    user_id     UUID,
    pii_type    VARCHAR(50)  NOT NULL,
    action      VARCHAR(20)  NOT NULL DEFAULT 'blocked',
    request_path VARCHAR(200),
    occurred_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX ON pii_violations(tenant_id, occurred_at DESC);
CREATE INDEX ON pii_violations(tenant_id, user_id, occurred_at DESC);
