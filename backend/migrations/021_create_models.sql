-- +goose Up
CREATE TABLE IF NOT EXISTS models (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'openrouter',
    tier TEXT NOT NULL DEFAULT 'free',
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    is_default BOOLEAN NOT NULL DEFAULT false,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Ensure at most one default model
CREATE UNIQUE INDEX IF NOT EXISTS idx_models_default ON models (is_default) WHERE is_default = true;

-- Seed initial catalog
INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order) VALUES
    ('nvidia/nemotron-3-super-120b-a12b:free', 'Nemotron 3 Super', 'openrouter', 'free', true, true, 10),
    ('moonshotai/kimi-k2.5', 'Kimi K2.5', 'openrouter', 'paid', true, false, 20),
    ('xiaomi/mimo-v2-pro', 'MiMo V2 Pro', 'openrouter', 'paid', true, false, 30),
    ('anthropic/claude-sonnet-4.6', 'Claude Sonnet 4.6', 'openrouter', 'paid', true, false, 100)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE models;
