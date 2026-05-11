BEGIN;

-- webhook_configs
ALTER TABLE webhook_configs
  DROP CONSTRAINT IF EXISTS webhook_configs_platform_check;
ALTER TABLE webhook_configs
  ADD CONSTRAINT webhook_configs_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

-- user_integrations
ALTER TABLE user_integrations
  DROP CONSTRAINT IF EXISTS user_integrations_platform_check;
ALTER TABLE user_integrations
  ADD CONSTRAINT user_integrations_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

-- output_events
ALTER TABLE output_events
  DROP CONSTRAINT IF EXISTS output_events_platform_check;
ALTER TABLE output_events
  ADD CONSTRAINT output_events_platform_check
  CHECK (platform IN ('github','jira','feishu','dingtalk','gitlab','confluence'));

COMMIT;
