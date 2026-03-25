-- +goose Up
ALTER TABLE vps_instances ADD COLUMN IF NOT EXISTS custom_caddyfile TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN custom_caddyfile;
