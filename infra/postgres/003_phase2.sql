-- Webhook integration configs (one per platform per tenant)
CREATE TABLE webhook_configs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    platform                 TEXT NOT NULL CHECK (platform IN ('github','jira','feishu','dingtalk')),
    webhook_secret_encrypted TEXT NOT NULL,
    event_weights            JSONB NOT NULL DEFAULT '{}',
    is_active                BOOLEAN NOT NULL DEFAULT TRUE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform)
);

-- User third-party account mappings
CREATE TABLE user_integrations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL CHECK (platform IN ('github','jira','feishu','dingtalk')),
    external_id TEXT NOT NULL,
    created_by  TEXT NOT NULL DEFAULT 'employee' CHECK (created_by IN ('employee','admin')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, platform, external_id)
);

-- Output events received from webhooks
-- I1 fix: added CHECK constraint on platform
CREATE TABLE output_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id           UUID REFERENCES users(id) ON DELETE SET NULL,
    platform          TEXT NOT NULL CHECK (platform IN ('github','jira','feishu','dingtalk')),
    event_type        TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    title             TEXT,
    weight            FLOAT NOT NULL DEFAULT 0,
    occurred_at       TIMESTAMPTZ NOT NULL,
    raw_payload       JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (platform, external_event_id)
);

CREATE INDEX idx_output_events_tenant_user ON output_events (tenant_id, user_id, occurred_at DESC);

-- Monthly KPI efficiency snapshots
-- I3 fix: user_id FK changed from ON DELETE CASCADE to ON DELETE RESTRICT
--         to preserve audit history when a user is deleted
CREATE TABLE efficiency_snapshots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    year_month          CHAR(7) NOT NULL,
    total_scu           FLOAT NOT NULL DEFAULT 0,
    total_output_weight FLOAT NOT NULL DEFAULT 0,
    efficiency_score    FLOAT NOT NULL DEFAULT 0,
    peer_group          TEXT NOT NULL,
    rank                INT NOT NULL DEFAULT 0,
    peer_count          INT NOT NULL DEFAULT 0,
    snapshot_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, user_id, year_month)
);

-- C1 fix: indexes for leaderboard query (tenant_id, year_month) and My-KPI query (user_id)
CREATE INDEX idx_efficiency_snapshots_tenant_month ON efficiency_snapshots (tenant_id, year_month);
CREATE INDEX idx_efficiency_snapshots_user ON efficiency_snapshots (user_id);

-- AI-Fuel reward transactions
-- I4 fix: user_id FK changed from ON DELETE CASCADE to ON DELETE RESTRICT
--         to prevent deleting ledger records when a user is deleted
-- I2 fix: ref_snapshot_id FK gets explicit ON DELETE SET NULL so snapshot
--         deletion does not block, but the transaction record survives
-- C2 fix: index on user_id for efficient per-user fuel lookups
CREATE TABLE fuel_transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    amount_scu      INTEGER NOT NULL,
    reason          TEXT NOT NULL,
    ref_snapshot_id UUID REFERENCES efficiency_snapshots(id) ON DELETE SET NULL,
    tier            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_fuel_transactions_user ON fuel_transactions (user_id);

-- Per-tenant AI-Fuel reward settings
CREATE TABLE fuel_settings (
    tenant_id       UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    top_10pct_bonus FLOAT NOT NULL DEFAULT 0.20,
    top_25pct_bonus FLOAT NOT NULL DEFAULT 0.10,
    top_50pct_bonus FLOAT NOT NULL DEFAULT 0.05,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
