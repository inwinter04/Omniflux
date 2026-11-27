-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_accounts
    ADD COLUMN api_protocol text NOT NULL DEFAULT 'openai_chat'
    CHECK (api_protocol IN (
        'openai_chat', 'openai_responses', 'anthropic_messages',
        'gemini_native', 'azure_openai', 'bedrock', 'vertex'
    ));

UPDATE provider_accounts SET api_protocol = CASE
    WHEN provider IN ('openai', 'openrouter', 'xai', 'volcengine-doubao', 'volcano-coding-plan', 'volcano-agent-plan', 'rightcode') THEN 'openai_responses'
    WHEN provider = 'anthropic' THEN 'anthropic_messages'
    WHEN provider = 'gemini' THEN 'gemini_native'
    WHEN provider = 'azure_openai' THEN 'azure_openai'
    WHEN provider = 'bedrock' THEN 'bedrock'
    WHEN provider = 'vertex' THEN 'vertex'
    ELSE 'openai_chat'
END;

ALTER TABLE model_candidates
    ADD COLUMN cache_read_cost_per_million numeric(12,6)
        CHECK (cache_read_cost_per_million IS NULL OR cache_read_cost_per_million >= 0),
    ADD COLUMN cache_write_cost_per_million numeric(12,6)
        CHECK (cache_write_cost_per_million IS NULL OR cache_write_cost_per_million >= 0);

ALTER TABLE route_policies
    ADD COLUMN session_affinity jsonb NOT NULL
        DEFAULT '{"enabled":true,"ttl_seconds":300,"key_strategy":"auto"}'::jsonb;

ALTER TABLE usage_records
    ADD COLUMN cache_creation_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN protocol text NOT NULL DEFAULT 'openai_chat'
        CHECK (protocol IN ('openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_native', 'openai_embeddings'));

CREATE OR REPLACE VIEW reporting_usage_records AS
SELECT id, request_id, organization_id, project_id, application_id, api_key_id,
       model_service_id, model_candidate_id, state, status_code, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, source,
       route_policy_id, error_type, error_message,
       cache_creation_tokens, protocol
FROM usage_records
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'prompt_debug'::text AS source,
       NULL::uuid, NULL::text, NULL::text,
       0::bigint AS cache_creation_tokens, 'openai_chat'::text AS protocol
FROM prompt_runs
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'agent_debug'::text AS source,
       NULL::uuid, NULL::text, NULL::text,
       0::bigint AS cache_creation_tokens, 'openai_chat'::text AS protocol
FROM agent_runs WHERE actor_type = 'user';

CREATE INDEX usage_records_protocol_time_idx ON usage_records(organization_id, protocol, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS usage_records_protocol_time_idx;
DROP VIEW IF EXISTS reporting_usage_records;

CREATE VIEW reporting_usage_records AS
SELECT id, request_id, organization_id, project_id, application_id, api_key_id,
       model_service_id, model_candidate_id, state, status_code, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, source,
       route_policy_id, error_type, error_message
FROM usage_records
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'prompt_debug'::text AS source,
       NULL::uuid, NULL::text, NULL::text
FROM prompt_runs
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'agent_debug'::text AS source,
       NULL::uuid, NULL::text, NULL::text
FROM agent_runs WHERE actor_type = 'user';

ALTER TABLE usage_records
    DROP COLUMN IF EXISTS protocol,
    DROP COLUMN IF EXISTS cache_creation_tokens;

ALTER TABLE route_policies
    DROP COLUMN IF EXISTS session_affinity;

ALTER TABLE model_candidates
    DROP COLUMN IF EXISTS cache_write_cost_per_million,
    DROP COLUMN IF EXISTS cache_read_cost_per_million;

ALTER TABLE provider_accounts
    DROP COLUMN IF EXISTS api_protocol;
-- +goose StatementEnd
