-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users
    ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled', 'locked', 'pending_email_verification'));

ALTER TABLE system_settings
    ADD COLUMN email_verification_required boolean NOT NULL DEFAULT false;

CREATE TABLE email_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT false,
    smtp_host text NOT NULL DEFAULT '',
    smtp_port int NOT NULL DEFAULT 587 CHECK (smtp_port BETWEEN 1 AND 65535),
    smtp_username text NOT NULL DEFAULT '',
    smtp_password_ciphertext bytea,
    from_address text NOT NULL DEFAULT '',
    from_name text NOT NULL DEFAULT '',
    encryption text NOT NULL DEFAULT 'starttls' CHECK (encryption IN ('starttls', 'tls', 'none')),
    public_base_url text NOT NULL DEFAULT '',
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO email_settings(singleton) VALUES(true) ON CONFLICT DO NOTHING;

CREATE TABLE email_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    purpose text NOT NULL CHECK (purpose IN ('email_verification', 'user_invitation', 'password_reset')),
    token_hash text NOT NULL UNIQUE,
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    email text NOT NULL,
    role text CHECK (role IS NULL OR role IN ('org_admin', 'project_admin', 'developer', 'viewer')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_tokens_purpose_email_idx ON email_tokens(purpose, email, created_at DESC);
CREATE INDEX email_tokens_user_idx ON email_tokens(user_id, purpose, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE email_tokens;
DROP TABLE email_settings;
ALTER TABLE system_settings DROP COLUMN email_verification_required;
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users
    ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled', 'locked'));
ALTER TABLE users DROP COLUMN email_verified_at;
-- +goose StatementEnd
