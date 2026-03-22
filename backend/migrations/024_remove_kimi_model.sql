-- +goose Up
UPDATE models SET is_enabled = false WHERE id = 'moonshotai/kimi-k2.5';

-- +goose Down
UPDATE models SET is_enabled = true WHERE id = 'moonshotai/kimi-k2.5';
