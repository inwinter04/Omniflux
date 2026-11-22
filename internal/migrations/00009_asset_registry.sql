-- +goose Up
-- +goose StatementBegin
CREATE TABLE assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    asset_type text NOT NULL CHECK (asset_type IN ('prompt_template')),
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    owner_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','disabled','archived')),
    latest_version int NOT NULL CHECK (latest_version > 0),
    current_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, asset_type, name)
);

CREATE TABLE asset_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    version int NOT NULL CHECK (version > 0),
    spec jsonb NOT NULL,
    source_version_id uuid REFERENCES asset_versions(id) ON DELETE RESTRICT,
    created_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by uuid REFERENCES users(id) ON DELETE RESTRICT,
    published_at timestamptz,
    CHECK ((published_at IS NULL) = (published_by IS NULL)),
    UNIQUE (asset_id, version)
);

ALTER TABLE assets
    ADD CONSTRAINT assets_current_version_fk FOREIGN KEY (current_version_id)
    REFERENCES asset_versions(id) ON DELETE RESTRICT;

CREATE TABLE asset_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id uuid NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('organization','department','project')),
    subject_id uuid NOT NULL,
    actions text[] NOT NULL CHECK (
        cardinality(actions) > 0 AND actions <@ ARRAY['read','invoke']::text[]
    ),
    created_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (asset_id, subject_type, subject_id)
);

CREATE INDEX assets_org_type_status_idx ON assets(organization_id, asset_type, status, updated_at DESC);
CREATE INDEX asset_versions_asset_created_idx ON asset_versions(asset_id, version DESC);
CREATE INDEX asset_grants_subject_idx ON asset_grants(organization_id, subject_type, subject_id);
CREATE INDEX asset_grants_asset_idx ON asset_grants(asset_id);

CREATE OR REPLACE FUNCTION protect_asset_version() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'asset_versions are immutable';
    END IF;
    IF NEW.asset_id IS DISTINCT FROM OLD.asset_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.spec IS DISTINCT FROM OLD.spec
       OR NEW.source_version_id IS DISTINCT FROM OLD.source_version_id
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'asset version content is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL
       AND (NEW.published_at IS DISTINCT FROM OLD.published_at
            OR NEW.published_by IS DISTINCT FROM OLD.published_by) THEN
        RAISE EXCEPTION 'asset version publication is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER asset_versions_protect_update BEFORE UPDATE ON asset_versions
FOR EACH ROW EXECUTE FUNCTION protect_asset_version();
CREATE TRIGGER asset_versions_protect_delete BEFORE DELETE ON asset_versions
FOR EACH ROW EXECUTE FUNCTION protect_asset_version();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE asset_grants;
ALTER TABLE assets DROP CONSTRAINT assets_current_version_fk;
DROP TABLE asset_versions;
DROP TABLE assets;
DROP FUNCTION protect_asset_version();
-- +goose StatementEnd
