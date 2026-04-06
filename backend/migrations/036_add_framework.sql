-- +goose Up
ALTER TABLE vps_instances ADD COLUMN framework TEXT NOT NULL DEFAULT 'openclaw';

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN framework;
