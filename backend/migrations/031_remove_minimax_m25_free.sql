-- +goose Up
DELETE FROM models WHERE id = 'minimax/minimax-m2.5:free';

-- +goose Down
INSERT INTO models (id, display_name, provider, tier, enabled, is_default, sort_order)
VALUES
    ('minimax/minimax-m2.5:free', 'MiniMax M2.5', 'openrouter', 'free', true, false, 110);
