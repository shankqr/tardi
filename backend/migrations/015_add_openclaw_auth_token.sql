-- +goose Up
ALTER TABLE vps_instances ADD COLUMN openclaw_auth_token TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN openclaw_auth_token;
