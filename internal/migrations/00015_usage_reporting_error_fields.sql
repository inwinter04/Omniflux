-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE VIEW reporting_usage_records AS
SELECT id, request_id, organization_id, project_id, application_id, api_key_id,
       model_service_id, model_candidate_id, state, status_code, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'api'::text AS source,
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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE VIEW reporting_usage_records AS
SELECT id, request_id, organization_id, project_id, application_id, api_key_id,
       model_service_id, model_candidate_id, state, status_code, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'api'::text AS source
FROM usage_records
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'prompt_debug'::text AS source
FROM prompt_runs
UNION ALL
SELECT id, id AS request_id, organization_id, NULL::uuid, NULL::uuid, NULL::uuid,
       model_service_id, NULL::uuid, status, NULL::int, provider, upstream_model,
       input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost, cost_currency,
       first_token_ms, total_ms, created_at, 'agent_debug'::text AS source
FROM agent_runs WHERE actor_type = 'user';
-- +goose StatementEnd
