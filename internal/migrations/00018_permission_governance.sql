-- +goose Up
-- +goose StatementBegin
CREATE TABLE permission_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    permissions text[] NOT NULL DEFAULT '{}',
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);
CREATE INDEX permission_groups_org_idx ON permission_groups(organization_id, created_at ASC);

ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('platform_admin','org_owner','org_admin','project_admin','developer','viewer','pending'));

ALTER TABLE users
    ADD COLUMN permission_group_id uuid REFERENCES permission_groups(id) ON DELETE RESTRICT;
CREATE INDEX users_permission_group_idx ON users(permission_group_id) WHERE permission_group_id IS NOT NULL;

CREATE TABLE user_permission_overrides (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission text NOT NULL,
    effect text NOT NULL CHECK (effect IN ('allow', 'deny')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, permission)
);

ALTER TABLE system_settings DROP CONSTRAINT system_settings_registration_role_check;
ALTER TABLE system_settings
    ADD CONSTRAINT system_settings_registration_role_check CHECK (registration_role IN ('pending', 'developer', 'viewer'));
ALTER TABLE system_settings
    ALTER COLUMN registration_role SET DEFAULT 'pending';

ALTER TABLE email_tokens DROP CONSTRAINT email_tokens_role_check;
ALTER TABLE email_tokens
    ADD CONSTRAINT email_tokens_role_check CHECK (role IS NULL OR role IN ('org_admin', 'project_admin', 'developer', 'viewer', 'pending'));
ALTER TABLE email_tokens
    ADD COLUMN permission_group_id uuid REFERENCES permission_groups(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE email_tokens DROP COLUMN permission_group_id;
ALTER TABLE email_tokens DROP CONSTRAINT email_tokens_role_check;
ALTER TABLE email_tokens
    ADD CONSTRAINT email_tokens_role_check CHECK (role IS NULL OR role IN ('org_admin', 'project_admin', 'developer', 'viewer'));

ALTER TABLE system_settings
    ALTER COLUMN registration_role SET DEFAULT 'viewer';
ALTER TABLE system_settings DROP CONSTRAINT system_settings_registration_role_check;
ALTER TABLE system_settings
    ADD CONSTRAINT system_settings_registration_role_check CHECK (registration_role IN ('developer', 'viewer'));

DROP TABLE user_permission_overrides;
DROP INDEX users_permission_group_idx;
ALTER TABLE users DROP COLUMN permission_group_id;
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('platform_admin','org_owner','org_admin','project_admin','developer','viewer'));

DROP TABLE permission_groups;
-- +goose StatementEnd
