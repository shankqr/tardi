-- +goose Up
-- Make codex/gpt-5.5 the platform default. Linking ChatGPT via the FE
-- (CodexConnect) is now the intended onboarding flow — OpenRouter access
-- becomes a Power User opt-in.
--
-- idx_models_default is a partial unique index on (is_default) WHERE
-- is_default = true, so we must clear the existing default *before*
-- inserting/updating the new one or the statement collides with itself.
UPDATE models SET is_default = false WHERE is_default = true;

INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order)
VALUES ('codex/gpt-5.5', 'GPT-5.5 (Codex)', 'codex', 'paid', true, true, 1)
ON CONFLICT (id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    provider     = EXCLUDED.provider,
    tier         = EXCLUDED.tier,
    is_enabled   = true,
    is_default   = true,
    sort_order   = 1;

-- +goose Down
DELETE FROM models WHERE id = 'codex/gpt-5.5';
UPDATE models SET is_default = true WHERE id = 'openai/gpt-5.4';
