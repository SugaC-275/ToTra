-- Migration 030: alert delivery configurations
-- Stores per-tenant channel configs for push alert delivery (Slack, email, webhook).

-- PostgreSQL has no "CREATE TYPE IF NOT EXISTS"; use the idempotent DO-block idiom.
DO $$ BEGIN
    CREATE TYPE alert_channel AS ENUM ('slack', 'email', 'webhook');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE alert_event_type AS ENUM ('budget_exceeded', 'budget_warning', 'compliance_violation', 'pii_spike');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS alert_delivery_configs (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT            NOT NULL,
    channel     alert_channel   NOT NULL,
    destination TEXT            NOT NULL,   -- Slack webhook URL / email address / HTTP URL
    event_types alert_event_type[] NOT NULL DEFAULT '{}',
    enabled     BOOLEAN         NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ     DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_configs_tenant ON alert_delivery_configs(tenant_id, enabled);
