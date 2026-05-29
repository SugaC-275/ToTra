CREATE TABLE saml_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL UNIQUE,
    entity_id       TEXT NOT NULL,
    idp_metadata_url TEXT,
    idp_metadata_xml TEXT,
    acs_url         TEXT NOT NULL,
    attribute_map   JSONB NOT NULL DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Attribute-to-bundle mapping: when IdP sends attribute_name=attribute_value, activate bundle_id
CREATE TABLE saml_attribute_bundle_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    attribute_name  TEXT NOT NULL,
    attribute_value TEXT NOT NULL,
    bundle_id       TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, attribute_name, attribute_value)
);
