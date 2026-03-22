-- +goose Up
UPDATE models SET sort_order = 12 WHERE id = 'xiaomi/mimo-v2-pro';

-- +goose Down
UPDATE models SET sort_order = 30 WHERE id = 'xiaomi/mimo-v2-pro';
