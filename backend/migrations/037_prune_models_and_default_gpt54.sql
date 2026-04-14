-- +goose Up
DELETE FROM models WHERE id IN (
    'nvidia/nemotron-3-super-120b-a12b:free',
    'qwen/qwen3.6-plus:free',
    'qwen/qwen3.5-397b-a17b',
    'z-ai/glm-5-turbo',
    'anthropic/claude-sonnet-4.6'
);

UPDATE models SET is_default = true WHERE id = 'openai/gpt-5.4';

-- +goose Down
UPDATE models SET is_default = false WHERE id = 'openai/gpt-5.4';

INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('nvidia/nemotron-3-super-120b-a12b:free', 'Nemotron 3 Super', 'openrouter', 'free', true, true, 10),
    ('qwen/qwen3.6-plus:free', 'Qwen 3.6 Plus', 'openrouter', 'free', true, false, 15),
    ('qwen/qwen3.5-397b-a17b', 'Qwen 3.5 397B', 'openrouter', 'paid', true, false, 50),
    ('z-ai/glm-5-turbo', 'GLM-5 Turbo', 'openrouter', 'paid', true, false, 70),
    ('anthropic/claude-sonnet-4.6', 'Claude Sonnet 4.6', 'openrouter', 'paid', true, false, 100)
ON CONFLICT (id) DO NOTHING;
