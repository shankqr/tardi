-- +goose Up
-- The codex CLI's `codex login status` output is just "Logged in using
-- ChatGPT" — no email — so this column was always written as NULL after
-- ChatGPT linking. The link state itself is captured by codex_linked_at.
ALTER TABLE vps_instances DROP COLUMN IF EXISTS codex_account_email;

-- +goose Down
ALTER TABLE vps_instances ADD COLUMN codex_account_email TEXT;
