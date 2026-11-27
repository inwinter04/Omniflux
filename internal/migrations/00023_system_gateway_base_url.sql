-- +goose Up
-- +goose StatementBegin
ALTER TABLE system_settings
    ADD COLUMN IF NOT EXISTS gateway_base_url text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE system_settings
    DROP COLUMN IF EXISTS gateway_base_url;
-- +goose StatementEnd
