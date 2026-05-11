BEGIN;

-- Add configurable retention window to each tenant (default 24 months)
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS data_retention_months INT NOT NULL DEFAULT 24;

-- Deletion requests submitted by employees
CREATE TABLE IF NOT EXISTS data_deletion_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_deletion_requests_tenant_status
    ON data_deletion_requests (tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_deletion_requests_user
    ON data_deletion_requests (user_id);

COMMIT;
