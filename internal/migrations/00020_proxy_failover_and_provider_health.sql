-- +goose Up
-- +goose StatementBegin
ALTER TABLE platform_proxies
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN probe_url text;

ALTER TABLE platform_proxy_providers DROP CONSTRAINT platform_proxy_providers_pkey;
ALTER TABLE platform_proxy_providers
    ADD COLUMN priority int NOT NULL DEFAULT 100 CHECK (priority >= 1),
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE platform_proxy_providers
    ADD PRIMARY KEY (provider_key, proxy_id);
CREATE INDEX platform_proxy_providers_provider_priority_idx
    ON platform_proxy_providers(provider_key, priority ASC, created_at ASC);

CREATE TABLE proxy_governance_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT true,
    probe_interval_seconds int NOT NULL DEFAULT 60 CHECK (probe_interval_seconds BETWEEN 15 AND 3600),
    fast_probe_interval_seconds int NOT NULL DEFAULT 10 CHECK (fast_probe_interval_seconds BETWEEN 3 AND 120),
    probe_timeout_seconds int NOT NULL DEFAULT 8 CHECK (probe_timeout_seconds BETWEEN 2 AND 60),
    fail_threshold int NOT NULL DEFAULT 3 CHECK (fail_threshold BETWEEN 1 AND 20),
    recover_threshold int NOT NULL DEFAULT 3 CHECK (recover_threshold BETWEEN 1 AND 20),
    switch_cooldown_seconds int NOT NULL DEFAULT 300 CHECK (switch_cooldown_seconds BETWEEN 0 AND 86400),
    failback_mode text NOT NULL DEFAULT 'sticky' CHECK (failback_mode IN ('sticky', 'auto_after_soak')),
    failback_soak_seconds int NOT NULL DEFAULT 1800 CHECK (failback_soak_seconds BETWEEN 60 AND 86400),
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO proxy_governance_settings(singleton) VALUES (true) ON CONFLICT DO NOTHING;

CREATE TABLE provider_proxy_states (
    provider_key text PRIMARY KEY,
    active_proxy_id uuid REFERENCES platform_proxies(id) ON DELETE SET NULL,
    last_switched_at timestamptz,
    last_switch_reason text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO provider_proxy_states(provider_key, active_proxy_id)
SELECT DISTINCT ON (provider_key) provider_key, proxy_id
FROM platform_proxy_providers
ORDER BY provider_key, priority ASC, created_at ASC
ON CONFLICT (provider_key) DO NOTHING;

CREATE TABLE proxy_health_states (
    provider_key text NOT NULL,
    proxy_id uuid NOT NULL REFERENCES platform_proxies(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'healthy', 'suspect', 'unhealthy')),
    consecutive_failures int NOT NULL DEFAULT 0,
    consecutive_successes int NOT NULL DEFAULT 0,
    ejection_count int NOT NULL DEFAULT 0,
    cooldown_until timestamptz,
    healthy_since timestamptz,
    last_latency_ms int,
    last_reason text,
    last_error_detail text,
    last_checked_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_key, proxy_id)
);

CREATE TABLE proxy_probe_checks (
    id bigserial PRIMARY KEY,
    proxy_id uuid NOT NULL REFERENCES platform_proxies(id) ON DELETE CASCADE,
    provider_key text NOT NULL,
    trigger text NOT NULL DEFAULT 'auto' CHECK (trigger IN ('auto', 'manual', 'fast_confirm')),
    ok boolean NOT NULL,
    status text NOT NULL CHECK (status IN ('healthy', 'degraded', 'unhealthy')),
    reason text NOT NULL,
    http_status int,
    latency_ms int,
    error_detail text,
    checked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX proxy_probe_checks_proxy_checked_idx
    ON proxy_probe_checks(proxy_id, checked_at DESC);
CREATE INDEX proxy_probe_checks_provider_proxy_checked_idx
    ON proxy_probe_checks(provider_key, proxy_id, checked_at DESC);
CREATE INDEX proxy_probe_checks_checked_at_idx
    ON proxy_probe_checks(checked_at);

CREATE TABLE proxy_failover_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_key text NOT NULL,
    from_proxy_id uuid REFERENCES platform_proxies(id) ON DELETE SET NULL,
    from_proxy_name text,
    to_proxy_id uuid REFERENCES platform_proxies(id) ON DELETE SET NULL,
    to_proxy_name text,
    trigger text NOT NULL CHECK (trigger IN ('auto_failover', 'auto_failback', 'manual', 'config_change')),
    reason text NOT NULL,
    detail text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX proxy_failover_events_provider_created_idx
    ON proxy_failover_events(provider_key, created_at DESC);
CREATE INDEX proxy_failover_events_created_idx
    ON proxy_failover_events(created_at DESC);

CREATE TABLE provider_health_check_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT true,
    interval_seconds int NOT NULL DEFAULT 300 CHECK (interval_seconds BETWEEN 60 AND 86400),
    timeout_seconds int NOT NULL DEFAULT 15 CHECK (timeout_seconds BETWEEN 3 AND 60),
    degraded_latency_ms int NOT NULL DEFAULT 5000 CHECK (degraded_latency_ms BETWEEN 500 AND 60000),
    failure_threshold int NOT NULL DEFAULT 2 CHECK (failure_threshold BETWEEN 1 AND 10),
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO provider_health_check_settings(singleton) VALUES (true) ON CONFLICT DO NOTHING;

CREATE TABLE provider_account_health_states (
    provider_account_id uuid PRIMARY KEY REFERENCES provider_accounts(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'healthy', 'degraded', 'unhealthy', 'unsupported')),
    reason text,
    http_status int,
    latency_ms int,
    proxy_id uuid REFERENCES platform_proxies(id) ON DELETE SET NULL,
    proxy_name text,
    error_detail text,
    consecutive_failures int NOT NULL DEFAULT 0,
    last_checked_at timestamptz,
    last_healthy_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE provider_account_health_checks (
    id bigserial PRIMARY KEY,
    provider_account_id uuid NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider text NOT NULL,
    trigger text NOT NULL DEFAULT 'auto' CHECK (trigger IN ('auto', 'manual')),
    ok boolean NOT NULL,
    status text NOT NULL CHECK (status IN ('healthy', 'degraded', 'unhealthy', 'unsupported')),
    reason text NOT NULL,
    http_status int,
    latency_ms int,
    proxy_id uuid REFERENCES platform_proxies(id) ON DELETE SET NULL,
    proxy_name text,
    error_detail text,
    checked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX provider_account_health_checks_account_checked_idx
    ON provider_account_health_checks(provider_account_id, checked_at DESC);
CREATE INDEX provider_account_health_checks_org_checked_idx
    ON provider_account_health_checks(organization_id, checked_at DESC);
CREATE INDEX provider_account_health_checks_checked_at_idx
    ON provider_account_health_checks(checked_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE provider_account_health_checks;
DROP TABLE provider_account_health_states;
DROP TABLE provider_health_check_settings;
DROP TABLE proxy_failover_events;
DROP TABLE proxy_probe_checks;
DROP TABLE proxy_health_states;
DROP TABLE provider_proxy_states;
DROP TABLE proxy_governance_settings;

DELETE FROM platform_proxy_providers a
USING (
    SELECT ctid, row_number() OVER (PARTITION BY provider_key ORDER BY priority ASC, created_at ASC) AS rn
    FROM platform_proxy_providers
) d
WHERE a.ctid = d.ctid AND d.rn > 1;

DROP INDEX platform_proxy_providers_provider_priority_idx;
ALTER TABLE platform_proxy_providers DROP CONSTRAINT platform_proxy_providers_pkey;
ALTER TABLE platform_proxy_providers
    DROP COLUMN created_at,
    DROP COLUMN enabled,
    DROP COLUMN priority;
ALTER TABLE platform_proxy_providers ADD PRIMARY KEY (provider_key);

ALTER TABLE platform_proxies
    DROP COLUMN probe_url,
    DROP COLUMN enabled;
-- +goose StatementEnd
