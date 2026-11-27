-- +goose Up
ALTER TABLE model_services
    ADD COLUMN expose_router boolean NOT NULL DEFAULT false,
    ADD COLUMN router_model_id text;

CREATE UNIQUE INDEX model_services_org_router_model_id_uidx
    ON model_services (organization_id, router_model_id)
    WHERE expose_router = true AND router_model_id IS NOT NULL;

ALTER TABLE route_policies
    ADD COLUMN orchestrator_config jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE route_policies
    DROP COLUMN IF EXISTS orchestrator_config;

DROP INDEX IF EXISTS model_services_org_router_model_id_uidx;

ALTER TABLE model_services
    DROP COLUMN IF EXISTS router_model_id,
    DROP COLUMN IF EXISTS expose_router;
