-- +goose Up
-- Shift Claude Opus down to make room
UPDATE models SET sort_order = 17 WHERE id = 'anthropic/claude-opus-4.6';
UPDATE models SET sort_order = 16 WHERE id = 'z-ai/glm-5-turbo';

-- +goose Down
UPDATE models SET sort_order = 16 WHERE id = 'anthropic/claude-opus-4.6';
UPDATE models SET sort_order = 70 WHERE id = 'z-ai/glm-5-turbo';
