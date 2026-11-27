-- +goose Up
-- +goose StatementBegin
ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS expose_upstream_balance_to_clients boolean NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE system_settings
    DROP COLUMN IF EXISTS expose_upstream_balance_to_clients;
-- +goose StatementEnd
