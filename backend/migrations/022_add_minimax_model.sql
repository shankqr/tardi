-- +goose Up
INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('minimax/minimax-m2.7', 'MiniMax M2.7', 'openrouter', 'paid', true, false, 15)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM models WHERE id = 'minimax/minimax-m2.7';
