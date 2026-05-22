-- Add review_note and updated_at to the existing quota_requests table.
-- The table was created in 001_init.sql with status pending/approved/rejected.
ALTER TABLE quota_requests
    ADD COLUMN IF NOT EXISTS review_note TEXT,
    ADD COLUMN IF NOT EXISTS updated_at  TIMESTAMPTZ DEFAULT NOW();

-- Backfill updated_at for existing rows
UPDATE quota_requests SET updated_at = created_at WHERE updated_at IS NULL;

-- Index for per-user history lookups
CREATE INDEX IF NOT EXISTS idx_quota_requests_user    ON quota_requests(tenant_id, user_id);
-- Partial index for admin pending-queue queries
CREATE INDEX IF NOT EXISTS idx_quota_requests_pending ON quota_requests(tenant_id, status) WHERE status = 'pending';
