-- +goose Up
INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('minimax/minimax-m2.5:free', 'MiniMax M2.5', 'openrouter', 'free', true, false, 110)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM models WHERE id = 'minimax/minimax-m2.5:free';
