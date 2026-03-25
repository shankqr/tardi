-- +goose Up
ALTER TABLE vps_instances ADD COLUMN agent_error text;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN agent_error;
