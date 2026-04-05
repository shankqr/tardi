-- +goose Up
INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('qwen/qwen3.6-plus:free', 'Qwen 3.6 Plus', 'openrouter', 'free', true, false, 15),
    ('arcee-ai/trinity-large-thinking', 'Trinity Large Thinking', 'openrouter', 'paid', true, false, 55)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM models WHERE id IN ('qwen/qwen3.6-plus:free', 'arcee-ai/trinity-large-thinking');
