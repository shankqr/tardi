-- +goose Up
ALTER TABLE vps_instances ADD COLUMN codex_linked_at TIMESTAMPTZ;
ALTER TABLE vps_instances ADD COLUMN codex_account_email TEXT;

-- +goose Down
ALTER TABLE vps_instances DROP COLUMN IF EXISTS codex_linked_at;
ALTER TABLE vps_instances DROP COLUMN IF EXISTS codex_account_email;
