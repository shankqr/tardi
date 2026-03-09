-- +goose Up
ALTER TABLE snapshots ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE snapshots DROP COLUMN is_system;
