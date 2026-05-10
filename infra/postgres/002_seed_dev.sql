-- infra/postgres/002_seed_dev.sql
-- Development seed data only — never run in production

INSERT INTO tenants (id, name, plan) VALUES
  ('00000000-0000-0000-0000-000000000001', 'Acme Corp', 'api_key');

-- Admin user
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role, job_role, department)
VALUES (
  '00000000-0000-0000-0000-000000000010',
  '00000000-0000-0000-0000-000000000001',
  'Admin User', 'admin@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-admin-key'), 'hex'),
  999999, 'admin', 'engineering-manager', 'platform'
);

-- Standard employee
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role, job_role, department)
VALUES (
  '00000000-0000-0000-0000-000000000011',
  '00000000-0000-0000-0000-000000000001',
  'Alice Engineer', 'alice@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-alice-key'), 'hex'),
  50000, 'standard', 'engineer', 'platform'
);

-- Model config
INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
VALUES (
  '00000000-0000-0000-0000-000000000020',
  '00000000-0000-0000-0000-000000000001',
  'gpt-4o', 'openai', 'dev-openai-key', 'https://api.openai.com/v1', 2.0
);

-- Usage records with conversation_id to test AIQ computation
-- Alice: 3 conversations across 12 working days
INSERT INTO usage_records (id, tenant_id, user_id, model_config_id, conversation_id,
  prompt_tokens, completion_tokens, scu_cost, usd_cost, response_ms, request_at)
VALUES
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000001',
   500,1500,0.02,0.0015,800,'2026-05-02 10:00:00+00'),
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000001',
   300,900,0.012,0.0009,600,'2026-05-02 10:05:00+00'),
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000002',
   800,2400,0.032,0.0024,1200,'2026-05-05 14:00:00+00'),
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000002',
   600,1800,0.024,0.0018,900,'2026-05-05 14:10:00+00'),
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000002',
   400,1200,0.016,0.0012,700,'2026-05-05 14:20:00+00'),
  (gen_random_uuid(),'00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011',
   '00000000-0000-0000-0000-000000000020','aaaaaaaa-0000-0000-0000-000000000003',
   700,2100,0.028,0.0021,1000,'2026-05-08 09:00:00+00');
