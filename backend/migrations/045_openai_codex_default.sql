-- +goose Up
-- OC 2026.5.x routes ChatGPT-linked Codex models through openai-codex/*.
UPDATE models SET is_default = false WHERE is_default = true;

INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order)
VALUES ('openai-codex/gpt-5.5', 'GPT-5.5 (Codex)', 'openai-codex', 'paid', true, true, 1)
ON CONFLICT (id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    provider     = EXCLUDED.provider,
    tier         = EXCLUDED.tier,
    is_enabled   = true,
    is_default   = true,
    sort_order   = 1,
    updated_at   = now();

UPDATE agent_configs
SET config = jsonb_set(
                 jsonb_set(config, '{model}', '"openai-codex/gpt-5.5"', true),
                 '{provider}', '"openai-codex"', true),
    updated_at = now()
WHERE config->>'model' IN ('codex/gpt-5.5', 'openai/gpt-5.5');

-- +goose Down
UPDATE models SET is_default = false WHERE is_default = true;
UPDATE models
SET is_default = true,
    updated_at = now()
WHERE id = 'codex/gpt-5.5';

UPDATE agent_configs
SET config = jsonb_set(
                 jsonb_set(config, '{model}', '"codex/gpt-5.5"', true),
                 '{provider}', '"codex"', true),
    updated_at = now()
WHERE config->>'model' = 'openai-codex/gpt-5.5';
