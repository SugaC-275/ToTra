CREATE TABLE IF NOT EXISTS siem_configs (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id        TEXT        NOT NULL,
  name             TEXT        NOT NULL,
  endpoint_url     TEXT        NOT NULL,
  api_key_encrypted TEXT       NOT NULL,
  event_types      TEXT[]      NOT NULL,
  is_active        BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_siem_configs_tenant ON siem_configs (tenant_id);

CREATE TABLE IF NOT EXISTS siem_delivery_queue (
  id             BIGSERIAL   PRIMARY KEY,
  tenant_id      TEXT        NOT NULL,
  siem_config_id UUID        NOT NULL REFERENCES siem_configs(id) ON DELETE CASCADE,
  event_type     TEXT        NOT NULL,
  payload        JSONB       NOT NULL,
  status         TEXT        NOT NULL DEFAULT 'pending'
                             CHECK (status IN ('pending','delivered','failed')),
  attempts       INT         NOT NULL DEFAULT 0,
  next_retry_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at   TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_siem_delivery_queue_lookup ON siem_delivery_queue (tenant_id, status, next_retry_at);
