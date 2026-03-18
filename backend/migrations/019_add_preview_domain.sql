-- +goose Up
ALTER TABLE vps_instances ADD COLUMN preview_domain TEXT;
ALTER TABLE vps_instances ADD COLUMN preview_dns_record_id TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN preview_domain;
ALTER TABLE vps_instances DROP COLUMN preview_dns_record_id;
