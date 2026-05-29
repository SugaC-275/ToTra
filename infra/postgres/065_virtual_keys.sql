CREATE TABLE virtual_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,  -- SHA-256 of the raw key
    model_alias TEXT,                  -- if set, override requested model with this
    budget_usd  NUMERIC(12,4),         -- NULL = inherit tenant budget
    pii_policy  TEXT DEFAULT 'inherit', -- 'inherit' | 'strict' | 'permissive'
    bundle_ids  TEXT[] DEFAULT '{}',   -- compliance bundles active for this key
    expires_at  TIMESTAMPTZ,
    created_by  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_active   BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX ON virtual_keys(key_hash) WHERE is_active = true;
CREATE INDEX ON virtual_keys(tenant_id) WHERE is_active = true;
