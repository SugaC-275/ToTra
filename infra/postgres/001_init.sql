CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Tenants (one per enterprise customer)
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL CHECK (plan IN ('api_key', 'sso')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Users (employees)
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    email           TEXT NOT NULL,
    employee_id     TEXT,
    auth_type       TEXT NOT NULL CHECK (auth_type IN ('api_key', 'sso')),
    api_key_hash    TEXT,
    quota_scu       INTEGER NOT NULL DEFAULT 100000,
    quota_reset_day INTEGER NOT NULL DEFAULT 1 CHECK (quota_reset_day BETWEEN 1 AND 28),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    role            TEXT NOT NULL DEFAULT 'standard' CHECK (role IN ('standard', 'senior', 'researcher', 'admin')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, email)
);

-- Model configurations (per tenant)
CREATE TABLE model_configs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    provider             TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini', 'local')),
    api_key_encrypted    TEXT,
    base_url             TEXT NOT NULL,
    scu_rate             FLOAT NOT NULL DEFAULT 1.0,
    allowed_roles        TEXT[] NOT NULL DEFAULT ARRAY['standard','senior','researcher','admin'],
    is_active            BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

-- Usage records (immutable, one row per request)
CREATE TABLE usage_records (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    user_id           UUID NOT NULL REFERENCES users(id),
    model_config_id   UUID NOT NULL REFERENCES model_configs(id),
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    scu_cost          FLOAT NOT NULL,
    usd_cost          FLOAT NOT NULL,
    response_ms       INTEGER NOT NULL DEFAULT 0,
    request_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_usage_tenant_user ON usage_records (tenant_id, user_id, request_at DESC);

-- Monthly summaries
CREATE TABLE monthly_summaries (
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    user_id        UUID NOT NULL REFERENCES users(id),
    year_month     CHAR(7) NOT NULL,
    total_scu      FLOAT NOT NULL DEFAULT 0,
    total_usd      FLOAT NOT NULL DEFAULT 0,
    request_count  INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, year_month)
);

-- Quota change requests
CREATE TABLE quota_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id),
    user_id      UUID NOT NULL REFERENCES users(id),
    requested_by UUID NOT NULL REFERENCES users(id),
    new_quota    INTEGER NOT NULL,
    reason       TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by  UUID REFERENCES users(id),
    reviewed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- IP allowlist per tenant
CREATE TABLE ip_allowlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cidr       TEXT NOT NULL,
    label      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
