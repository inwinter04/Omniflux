-- +goose Up
-- +goose StatementBegin
CREATE TABLE model_service_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_service_id uuid NOT NULL REFERENCES model_services(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('organization', 'department', 'project')),
    subject_id uuid NOT NULL,
    actions text[] NOT NULL CHECK (
        cardinality(actions) > 0 AND actions <@ ARRAY['read', 'invoke']::text[]
    ),
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_service_id, subject_type, subject_id)
);

CREATE INDEX model_service_grants_service_idx ON model_service_grants(model_service_id);
CREATE INDEX model_service_grants_subject_idx ON model_service_grants(organization_id, subject_type, subject_id);

INSERT INTO model_service_grants(model_service_id, organization_id, subject_type, subject_id, actions)
SELECT id, organization_id, 'organization', organization_id, ARRAY['read', 'invoke']::text[]
FROM model_services;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE model_service_grants;
-- +goose StatementEnd
