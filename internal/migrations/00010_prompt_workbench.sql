-- +goose Up
-- +goose StatementBegin
CREATE TABLE prompt_evaluation_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (asset_id, name)
);

CREATE TABLE prompt_evaluation_cases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_set_id uuid NOT NULL REFERENCES prompt_evaluation_sets(id) ON DELETE CASCADE,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    match_type text NOT NULL CHECK (match_type IN ('exact','contains','regex')),
    expected text NOT NULL,
    position int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE prompt_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id uuid NOT NULL,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    version_id uuid NOT NULL REFERENCES asset_versions(id) ON DELETE RESTRICT,
    evaluation_set_id uuid REFERENCES prompt_evaluation_sets(id) ON DELETE SET NULL,
    evaluation_case_id uuid REFERENCES prompt_evaluation_cases(id) ON DELETE SET NULL,
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('single','compare','evaluation')),
    model_service_id uuid NOT NULL REFERENCES model_services(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'running' CHECK (status IN ('running','success','failed','interrupted')),
    rendered_prompt text NOT NULL,
    response_text text NOT NULL DEFAULT '',
    structured_output jsonb,
    schema_valid boolean,
    error_message text,
    provider text,
    upstream_model text,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    cached_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens bigint NOT NULL DEFAULT 0,
    cost numeric(20,8) NOT NULL DEFAULT 0,
    first_token_ms int,
    total_ms int,
    score numeric(5,4),
    matched boolean,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX prompt_evaluation_cases_set_idx ON prompt_evaluation_cases(evaluation_set_id, position, created_at);
CREATE INDEX prompt_runs_asset_history_idx ON prompt_runs(organization_id, asset_id, created_at DESC);
CREATE INDEX prompt_runs_group_idx ON prompt_runs(group_id, created_at);
CREATE INDEX prompt_runs_running_idx ON prompt_runs(created_at) WHERE status = 'running';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE prompt_runs;
DROP TABLE prompt_evaluation_cases;
DROP TABLE prompt_evaluation_sets;
-- +goose StatementEnd
