-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_accounts ADD COLUMN billing_currency text NOT NULL DEFAULT 'USD' CHECK (billing_currency ~ '^[A-Z]{3}$');
ALTER TABLE model_candidates ADD COLUMN cost_currency text NOT NULL DEFAULT 'USD' CHECK (cost_currency ~ '^[A-Z]{3}$');
ALTER TABLE prompt_runs ADD COLUMN cost_currency text NOT NULL DEFAULT 'USD' CHECK (cost_currency ~ '^[A-Z]{3}$');
ALTER TABLE agent_runs ADD COLUMN cost_currency text NOT NULL DEFAULT 'USD' CHECK (cost_currency ~ '^[A-Z]{3}$');

CREATE VIEW reporting_usage_records AS
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

-- +goose Down
-- +goose StatementBegin
DROP VIEW reporting_usage_records;
ALTER TABLE agent_runs DROP COLUMN cost_currency;
ALTER TABLE prompt_runs DROP COLUMN cost_currency;
ALTER TABLE model_candidates DROP COLUMN cost_currency;
ALTER TABLE provider_accounts DROP COLUMN billing_currency;
-- +goose StatementEnd
