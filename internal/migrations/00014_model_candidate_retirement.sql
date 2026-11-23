-- +goose Up
-- +goose StatementBegin
ALTER TABLE model_candidates ADD COLUMN retired_at timestamptz;
CREATE INDEX model_candidates_active_idx ON model_candidates(model_service_id) WHERE retired_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX model_candidates_active_idx;
ALTER TABLE model_candidates DROP COLUMN retired_at;
-- +goose StatementEnd
