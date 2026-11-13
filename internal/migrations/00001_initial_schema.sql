-- +goose Up
-- +goose StatementBegin
CREATE TABLE config_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    slug text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE departments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    department_id uuid NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('org_owner','org_admin','project_admin','developer','viewer')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','locked')),
    failed_login_count int NOT NULL DEFAULT 0,
    locked_until timestamptz,
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_hash text NOT NULL UNIQUE,
    ip_address inet,
    user_agent text,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_active_idx ON sessions(user_id) WHERE revoked_at IS NULL;

CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    created_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name text NOT NULL,
    key_prefix text NOT NULL UNIQUE,
    secret_hash text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    expires_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_keys_org_project_idx ON api_keys(organization_id, project_id, status);

CREATE TABLE provider_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('openai','azure_openai','anthropic','gemini','vertex','bedrock','openai_compatible')),
    name text NOT NULL,
    region text,
    base_url text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE provider_credentials (
    provider_account_id uuid PRIMARY KEY REFERENCES provider_accounts(id) ON DELETE CASCADE,
    key_id text NOT NULL,
    ciphertext bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_endpoints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_account_id uuid NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    endpoint_type text NOT NULL DEFAULT 'default',
    url text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE model_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_service_id uuid NOT NULL REFERENCES model_services(id) ON DELETE CASCADE,
    provider_account_id uuid NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
    endpoint_id uuid REFERENCES model_endpoints(id) ON DELETE SET NULL,
    upstream_model text NOT NULL,
    priority int NOT NULL DEFAULT 100,
    weight numeric NOT NULL DEFAULT 1 CHECK (weight >= 0),
    input_cost_per_million numeric NOT NULL DEFAULT 0 CHECK (input_cost_per_million >= 0),
    output_cost_per_million numeric NOT NULL DEFAULT 0 CHECK (output_cost_per_million >= 0),
    max_input_tokens int,
    max_output_tokens int,
    quality_score numeric NOT NULL DEFAULT 0.5 CHECK (quality_score >= 0 AND quality_score <= 1),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_service_id, provider_account_id, upstream_model)
);

CREATE TABLE route_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL,
    version int NOT NULL DEFAULT 1,
    strategy text NOT NULL DEFAULT 'balanced' CHECK (strategy IN ('cost_first','quality_first','balanced')),
    constraints jsonb NOT NULL DEFAULT '{}'::jsonb,
    weights jsonb NOT NULL DEFAULT '{"cost":0.4,"quality":0.3,"latency":0.15,"reliability":0.15}'::jsonb,
    fallback_limit int NOT NULL DEFAULT 2 CHECK (fallback_limit >= 0),
    provider_timeout_ms int NOT NULL DEFAULT 60000,
    first_token_timeout_ms int NOT NULL DEFAULT 15000,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE budget_windows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scope_type text NOT NULL CHECK (scope_type IN ('organization','project','application','api_key')),
    scope_id uuid NOT NULL,
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    reserved_amount numeric(20,8) NOT NULL DEFAULT 0 CHECK (reserved_amount >= 0),
    spent_amount numeric(20,8) NOT NULL DEFAULT 0 CHECK (spent_amount >= 0),
    limit_amount numeric(20,8) NOT NULL CHECK (limit_amount >= 0),
    currency text NOT NULL DEFAULT 'USD',
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scope_type, scope_id, window_start)
);

CREATE TABLE budget_reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_window_id uuid NOT NULL REFERENCES budget_windows(id) ON DELETE CASCADE,
    request_id uuid NOT NULL UNIQUE,
    estimated_amount numeric(20,8) NOT NULL CHECK (estimated_amount >= 0),
    final_amount numeric(20,8),
    state text NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved','settled','released','failed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    settled_at timestamptz
);

CREATE TABLE usage_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL UNIQUE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE RESTRICT,
    api_key_id uuid NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    model_service_id uuid NOT NULL REFERENCES model_services(id) ON DELETE RESTRICT,
    model_candidate_id uuid REFERENCES model_candidates(id) ON DELETE SET NULL,
    route_policy_id uuid REFERENCES route_policies(id) ON DELETE SET NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','success','failed','interrupted')),
    status_code int,
    provider text,
    upstream_model text,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    cached_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens bigint NOT NULL DEFAULT 0,
    cost numeric(20,8) NOT NULL DEFAULT 0,
    cost_currency text NOT NULL DEFAULT 'USD',
    first_token_ms int,
    total_ms int,
    usage_source text,
    error_type text,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz
);
CREATE INDEX usage_records_reporting_idx ON usage_records(organization_id, project_id, created_at DESC);
CREATE INDEX usage_records_pending_idx ON usage_records(created_at) WHERE state = 'pending';
CREATE UNIQUE INDEX usage_records_immutable_final_idx ON usage_records(request_id) WHERE state IN ('success','failed','interrupted');

CREATE TABLE policy_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL UNIQUE REFERENCES usage_records(request_id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    api_key_id uuid NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    route_policy_id uuid NOT NULL REFERENCES route_policies(id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('allowed','rejected','failed')),
    reason text NOT NULL,
    candidates jsonb NOT NULL DEFAULT '[]'::jsonb,
    selected_candidate_id uuid,
    fallback_candidate_ids uuid[] NOT NULL DEFAULT '{}',
    attempt_trail jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    actor_type text NOT NULL DEFAULT 'user' CHECK (actor_type IN ('user','api_key','system')),
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text,
    result text NOT NULL DEFAULT 'success' CHECK (result IN ('success','failure')),
    before_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    after_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip_address inet,
    user_agent text,
    request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org_time_idx ON audit_events(organization_id, created_at DESC);

CREATE OR REPLACE FUNCTION prevent_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();

INSERT INTO config_state(singleton) VALUES(true) ON CONFLICT DO NOTHING;


-- +goose StatementEnd
