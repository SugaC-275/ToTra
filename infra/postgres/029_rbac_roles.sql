DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('admin', 'dept_admin', 'compliance_officer', 'auditor', 'user');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS user_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role user_role NOT NULL,
    department TEXT,  -- scope: null = org-wide, value = specific dept
    granted_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(tenant_id, user_id, role, department)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_lookup ON user_roles(tenant_id, user_id);
