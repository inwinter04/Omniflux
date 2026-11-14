-- +goose Up
ALTER TABLE api_keys
    ADD COLUMN deleted_at timestamptz;
