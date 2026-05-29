-- Feature 2: Spend Tagging — add tags column to request_logs
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tags TEXT[] DEFAULT '{}';
CREATE INDEX IF NOT EXISTS request_logs_tags_gin ON request_logs USING GIN(tags) WHERE tags != '{}';

-- Feature 4: Slack/PagerDuty — add webhook_type to webhook_configs
ALTER TABLE webhook_configs ADD COLUMN IF NOT EXISTS webhook_type TEXT NOT NULL DEFAULT 'webhook';
-- 'webhook' | 'slack' | 'pagerduty'

-- Feature 3: Policy log action — structured event persistence
CREATE TABLE IF NOT EXISTS policy_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    user_id         TEXT,
    rule_name       TEXT NOT NULL,
    action          TEXT NOT NULL,  -- 'block' | 'log'
    matched_pattern TEXT,
    request_id      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS policy_events_tenant_created ON policy_events(tenant_id, created_at DESC);
