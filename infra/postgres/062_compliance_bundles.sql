CREATE TABLE compliance_bundle_activations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    bundle_id TEXT NOT NULL,
    activated_by TEXT NOT NULL,
    activated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_active BOOLEAN NOT NULL DEFAULT true,
    config_snapshot JSONB,
    UNIQUE(tenant_id, bundle_id)
);
