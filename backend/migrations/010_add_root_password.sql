-- +goose Up
ALTER TABLE vps_instances ADD COLUMN root_password TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN root_password;
