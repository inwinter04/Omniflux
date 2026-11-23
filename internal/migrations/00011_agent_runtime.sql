-- +goose Up
-- +goose StatementBegin
ALTER TABLE assets DROP CONSTRAINT assets_asset_type_check;
ALTER TABLE assets ADD CONSTRAINT assets_asset_type_check CHECK (asset_type IN ('prompt_template', 'agent'));

CREATE TABLE agent_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    agent_version_id uuid NOT NULL REFERENCES asset_versions(id) ON DELETE RESTRICT,
    channel text NOT NULL CHECK (channel IN ('debug', 'api')),
    title text NOT NULL DEFAULT '新对话',
    variables jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by uuid REFERENCES users(id) ON DELETE RESTRICT,
    project_id uuid REFERENCES projects(id) ON DELETE RESTRICT,
    application_id uuid REFERENCES applications(id) ON DELETE RESTRICT,
    api_key_id uuid REFERENCES api_keys(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (channel = 'debug' AND created_by IS NOT NULL AND project_id IS NULL AND application_id IS NULL AND api_key_id IS NULL)
        OR (channel = 'api' AND created_by IS NULL AND project_id IS NOT NULL AND application_id IS NOT NULL AND api_key_id IS NOT NULL)
    )
);

CREATE TABLE agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    session_id uuid NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    version_id uuid NOT NULL REFERENCES asset_versions(id) ON DELETE RESTRICT,
    model_service_id uuid NOT NULL REFERENCES model_services(id) ON DELETE RESTRICT,
    actor_type text NOT NULL CHECK (actor_type IN ('user', 'api_key')),
    actor_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    project_id uuid REFERENCES projects(id) ON DELETE RESTRICT,
    application_id uuid REFERENCES applications(id) ON DELETE RESTRICT,
    api_key_id uuid REFERENCES api_keys(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'failed', 'interrupted')),
    request_text text NOT NULL,
    rendered_prompt text NOT NULL,
    response_text text NOT NULL DEFAULT '',
    error_message text,
    provider text,
    upstream_model text,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    cached_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens bigint NOT NULL DEFAULT 0,
    cost numeric(20, 8) NOT NULL DEFAULT 0,
    first_token_ms int,
    total_ms int,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (actor_type = 'user' AND actor_user_id IS NOT NULL AND project_id IS NULL AND application_id IS NULL AND api_key_id IS NULL)
        OR (actor_type = 'api_key' AND actor_user_id IS NULL AND project_id IS NOT NULL AND application_id IS NOT NULL AND api_key_id IS NOT NULL)
    )
);

CREATE TABLE agent_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    agent_run_id uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX agent_sessions_asset_owner_idx ON agent_sessions(organization_id, asset_id, channel, created_by, updated_at DESC);
CREATE INDEX agent_sessions_project_idx ON agent_sessions(organization_id, project_id, asset_id, updated_at DESC);
CREATE INDEX agent_messages_session_idx ON agent_messages(session_id, created_at, id);
CREATE INDEX agent_runs_asset_history_idx ON agent_runs(organization_id, asset_id, created_at DESC);
CREATE INDEX agent_runs_running_idx ON agent_runs(created_at) WHERE status = 'running';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE agent_messages;
DROP TABLE agent_runs;
DROP TABLE agent_sessions;
UPDATE assets SET current_version_id = NULL WHERE asset_type = 'agent';
DELETE FROM asset_versions WHERE asset_id IN (SELECT id FROM assets WHERE asset_type = 'agent');
DELETE FROM assets WHERE asset_type = 'agent';
ALTER TABLE assets DROP CONSTRAINT assets_asset_type_check;
ALTER TABLE assets ADD CONSTRAINT assets_asset_type_check CHECK (asset_type IN ('prompt_template'));
-- +goose StatementEnd
