-- +goose Up
ALTER TABLE vps_instances ADD COLUMN agent_status TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN agent_status;
