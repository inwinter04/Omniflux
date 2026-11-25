-- +goose Up
-- +goose StatementBegin
CREATE TABLE system_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    public_registration_enabled boolean NOT NULL DEFAULT false,
    registration_organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    registration_role text NOT NULL DEFAULT 'viewer' CHECK (registration_role IN ('developer', 'viewer')),
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO system_settings(singleton) VALUES(true) ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE system_settings;
-- +goose StatementEnd
