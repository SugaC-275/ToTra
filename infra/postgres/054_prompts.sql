-- Prompt management: versioned prompt templates
CREATE TABLE IF NOT EXISTS prompt_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    content     TEXT NOT NULL,           -- the prompt template text
    variables   TEXT[] NOT NULL DEFAULT '{}',  -- e.g. ['user_name','context']
    model       TEXT,                    -- optional: default model for this prompt
    tags        TEXT[] NOT NULL DEFAULT '{}',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name, version)
);
CREATE INDEX IF NOT EXISTS idx_prompts_tenant ON prompt_templates(tenant_id, name, version DESC);
