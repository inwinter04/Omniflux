-- +goose Up
ALTER TABLE budget_reservations
    DROP CONSTRAINT budget_reservations_request_id_key;
ALTER TABLE budget_reservations
    ADD CONSTRAINT budget_reservations_window_request_key UNIQUE (budget_window_id, request_id);
