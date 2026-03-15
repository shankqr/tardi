-- +goose Up
ALTER TABLE vps_instances ADD COLUMN domain TEXT;
ALTER TABLE vps_instances ADD COLUMN dns_record_id TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN IF EXISTS domain;
ALTER TABLE vps_instances DROP COLUMN IF EXISTS dns_record_id;
