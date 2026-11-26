-- +goose Up
-- +goose StatementBegin
CREATE TABLE fx_rate_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    auto_sync_enabled boolean NOT NULL DEFAULT true,
    sync_interval_hours int NOT NULL DEFAULT 12 CHECK (sync_interval_hours BETWEEN 1 AND 168),
    primary_source text NOT NULL DEFAULT 'frankfurter_ecb',
    fallback_source text NOT NULL DEFAULT 'open_er_api',
    last_synced_at timestamptz,
    last_sync_status text NOT NULL DEFAULT 'never' CHECK (last_sync_status IN ('never','ok','partial','error')),
    last_sync_source text,
    last_sync_error text,
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO fx_rate_settings(singleton) VALUES (true) ON CONFLICT DO NOTHING;

CREATE TABLE fx_rates (
    currency text PRIMARY KEY CHECK (currency ~ '^[A-Z]{3}$'),
    rate_per_usd numeric(20,8) NOT NULL CHECK (rate_per_usd > 0),
    source text NOT NULL DEFAULT 'builtin' CHECK (source IN ('builtin','frankfurter_ecb','open_er_api','manual')),
    effective_date date,
    manual_override boolean NOT NULL DEFAULT false,
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO fx_rates(currency, rate_per_usd, source) VALUES
    ('USD', 1.00000000, 'builtin'),
    ('CNY', 7.20000000, 'builtin'),
    ('EUR', 0.92000000, 'builtin'),
    ('GBP', 0.79000000, 'builtin'),
    ('JPY', 154.00000000, 'builtin'),
    ('HKD', 7.80000000, 'builtin'),
    ('TWD', 32.50000000, 'builtin'),
    ('KRW', 1380.00000000, 'builtin')
ON CONFLICT (currency) DO NOTHING;

ALTER TABLE model_candidates
    ADD COLUMN context_window int CHECK (context_window IS NULL OR context_window > 0),
    ADD COLUMN capabilities text[] NOT NULL DEFAULT '{}',
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}';

ALTER TABLE route_policies DROP CONSTRAINT IF EXISTS route_policies_strategy_check;
ALTER TABLE route_policies
    ADD CONSTRAINT route_policies_strategy_check CHECK (
        strategy IN (
            'balanced', 'cost_first', 'quality_first',
            'latency_first', 'reliability_first',
            'weighted', 'round_robin', 'strict_failover', 'custom'
        )
    );
ALTER TABLE route_policies
    ADD COLUMN compare_currency text NOT NULL DEFAULT 'USD' CHECK (compare_currency ~ '^[A-Z]{3}$'),
    ADD COLUMN rules jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN retry_policy jsonb NOT NULL DEFAULT '{"retry_429":true,"retry_5xx":true,"retry_timeout":true,"fallback_on_auth_error":true,"fallback_on_not_found":true,"fallback_on_rate_limit":true,"fallback_on_server_error":true,"fallback_on_ttft_timeout":true,"fallback_on_client_error":false,"max_retries_per_candidate":0}'::jsonb,
    ADD COLUMN circuit_breaker jsonb NOT NULL DEFAULT '{"enabled":true,"failure_threshold":5,"cooldown_seconds":30,"half_open_max_calls":1}'::jsonb,
    ADD COLUMN baseline_candidate_id uuid REFERENCES model_candidates(id) ON DELETE SET NULL;

ALTER TABLE usage_records
    ALTER COLUMN project_id DROP NOT NULL,
    ALTER COLUMN application_id DROP NOT NULL,
    ALTER COLUMN api_key_id DROP NOT NULL,
    ADD COLUMN source text NOT NULL DEFAULT 'api' CHECK (source IN ('api','agent_api','route_playground')),
    ADD COLUMN actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE policy_decisions
    ALTER COLUMN api_key_id DROP NOT NULL,
    ADD COLUMN policy_version int,
    ADD COLUMN strategy text,
    ADD COLUMN matched_rule_id text,
    ADD COLUMN matched_rule_name text,
    ADD COLUMN decision_detail jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE OR REPLACE VIEW reporting_usage_records AS
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

CREATE INDEX usage_records_policy_time_idx ON usage_records(organization_id, route_policy_id, created_at DESC);
CREATE INDEX usage_records_candidate_time_idx ON usage_records(model_candidate_id, created_at DESC);
CREATE INDEX policy_decisions_policy_time_idx ON policy_decisions(organization_id, route_policy_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS policy_decisions_policy_time_idx;
DROP INDEX IF EXISTS usage_records_candidate_time_idx;
DROP INDEX IF EXISTS usage_records_policy_time_idx;

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

DELETE FROM policy_decisions WHERE api_key_id IS NULL;
ALTER TABLE policy_decisions
    DROP COLUMN IF EXISTS decision_detail,
    DROP COLUMN IF EXISTS matched_rule_name,
    DROP COLUMN IF EXISTS matched_rule_id,
    DROP COLUMN IF EXISTS strategy,
    DROP COLUMN IF EXISTS policy_version,
    ALTER COLUMN api_key_id SET NOT NULL;

DELETE FROM usage_records WHERE api_key_id IS NULL OR project_id IS NULL OR application_id IS NULL;
ALTER TABLE usage_records
    DROP COLUMN IF EXISTS actor_user_id,
    DROP COLUMN IF EXISTS source,
    ALTER COLUMN api_key_id SET NOT NULL,
    ALTER COLUMN application_id SET NOT NULL,
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE route_policies
    DROP COLUMN IF EXISTS baseline_candidate_id,
    DROP COLUMN IF EXISTS circuit_breaker,
    DROP COLUMN IF EXISTS retry_policy,
    DROP COLUMN IF EXISTS rules,
    DROP COLUMN IF EXISTS compare_currency;
UPDATE route_policies SET strategy = 'balanced' WHERE strategy NOT IN ('cost_first','quality_first','balanced');
ALTER TABLE route_policies DROP CONSTRAINT IF EXISTS route_policies_strategy_check;
ALTER TABLE route_policies ADD CONSTRAINT route_policies_strategy_check CHECK (strategy IN ('cost_first','quality_first','balanced'));

ALTER TABLE model_candidates
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS context_window;

DROP TABLE IF EXISTS fx_rates;
DROP TABLE IF EXISTS fx_rate_settings;
-- +goose StatementEnd
