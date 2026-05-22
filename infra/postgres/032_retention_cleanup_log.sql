-- Track last retention cleanup run per tenant in existing cost-settings table.
ALTER TABLE tenant_cost_settings
    ADD COLUMN IF NOT EXISTS last_cleanup_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_cleanup_rows_deleted BIGINT;
