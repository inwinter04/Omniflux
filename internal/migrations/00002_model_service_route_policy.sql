-- +goose Up
-- +goose StatementBegin
ALTER TABLE model_services
    ADD COLUMN route_policy_id uuid REFERENCES route_policies(id) ON DELETE SET NULL;
-- +goose StatementEnd
