-- +goose Up
UPDATE models SET sort_order = 16 WHERE id = 'anthropic/claude-opus-4.6';

-- +goose Down
UPDATE models SET sort_order = 60 WHERE id = 'anthropic/claude-opus-4.6';
