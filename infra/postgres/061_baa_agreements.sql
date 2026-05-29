CREATE TABLE baa_agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    signed_by_user_id TEXT NOT NULL,
    signed_by_name TEXT NOT NULL,
    signed_by_email TEXT NOT NULL,
    signed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    agreement_version TEXT NOT NULL DEFAULT 'v1.0',
    ip_address TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    UNIQUE(tenant_id, agreement_version)
);
CREATE INDEX baa_agreements_tenant ON baa_agreements(tenant_id) WHERE is_active = true;
