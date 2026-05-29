-- 061: sub-tenant departments
-- Adds department-level isolation under each organization (tenant).
-- Existing tenant_id values remain as-is; departments are an additive layer.

CREATE TABLE departments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    slug         TEXT NOT NULL,          -- URL-safe identifier, used in JWT dept_id claim
    budget_usd   NUMERIC(12,2),
    rpm_limit    INT,
    tpm_limit    INT,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, slug)
);

CREATE INDEX departments_tenant ON departments(tenant_id);

-- Users can optionally be assigned to a department within their tenant.
ALTER TABLE users ADD COLUMN IF NOT EXISTS department_id UUID REFERENCES departments(id) ON DELETE SET NULL;

CREATE INDEX users_dept ON users(department_id) WHERE department_id IS NOT NULL;
