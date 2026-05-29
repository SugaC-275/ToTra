-- Expand provider CHECK constraint to include all new providers.
ALTER TABLE model_configs DROP CONSTRAINT IF EXISTS model_configs_provider_check;
ALTER TABLE model_configs ADD CONSTRAINT model_configs_provider_check
    CHECK (provider IN (
        'openai','anthropic','gemini','local',
        'azure','bedrock','vertex','cohere',
        'elevenlabs','deepgram',
        'databricks','watsonx','cloudflare'
    ));
