-- +goose Up
INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('openai/gpt-5.4', 'GPT-5.4', 'openrouter', 'paid', true, false, 40),
    ('qwen/qwen3.5-397b-a17b', 'Qwen 3.5 397B', 'openrouter', 'paid', true, false, 50),
    ('anthropic/claude-opus-4.6', 'Claude Opus 4.6', 'openrouter', 'paid', true, false, 60),
    ('z-ai/glm-5-turbo', 'GLM-5 Turbo', 'openrouter', 'paid', true, false, 70)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM models WHERE id IN ('openai/gpt-5.4', 'qwen/qwen3.5-397b-a17b', 'anthropic/claude-opus-4.6', 'z-ai/glm-5-turbo');
