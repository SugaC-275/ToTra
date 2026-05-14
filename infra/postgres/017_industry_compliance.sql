CREATE TABLE IF NOT EXISTS eu_ai_act_checklist (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    item_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'not_assessed',
    notes TEXT NOT NULL DEFAULT '',
    assessed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, item_key)
);
CREATE INDEX IF NOT EXISTS idx_eu_checklist_tenant ON eu_ai_act_checklist(tenant_id);
