-- +goose Up
ALTER TABLE api_keys
    ADD COLUMN rate_limit_per_min int CHECK (rate_limit_per_min IS NULL OR rate_limit_per_min > 0);