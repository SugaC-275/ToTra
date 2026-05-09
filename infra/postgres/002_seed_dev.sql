-- infra/postgres/002_seed_dev.sql
-- Development seed data only — never run in production

-- Tenant: Acme Corp (API Key plan)
INSERT INTO tenants (id, name, plan) VALUES
  ('00000000-0000-0000-0000-000000000001', 'Acme Corp', 'api_key');

-- Admin user
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role)
VALUES (
  '00000000-0000-0000-0000-000000000010',
  '00000000-0000-0000-0000-000000000001',
  'Admin User', 'admin@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-admin-key'), 'hex'),
  999999, 'admin'
);

-- Standard employee
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role)
VALUES (
  '00000000-0000-0000-0000-000000000011',
  '00000000-0000-0000-0000-000000000001',
  'Alice Engineer', 'alice@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-alice-key'), 'hex'),
  50000, 'standard'
);

-- OpenAI model config (api_key_encrypted is plaintext here for dev ease)
INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
VALUES (
  '00000000-0000-0000-0000-000000000020',
  '00000000-0000-0000-0000-000000000001',
  'gpt-4o', 'openai', 'dev-openai-key', 'https://api.openai.com/v1', 2.0
);
