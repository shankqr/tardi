-- +goose Up

-- Global platform settings (single-row key-value store)
CREATE TABLE IF NOT EXISTS platform_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO platform_settings (key, value) VALUES ('target_openclaw_version', 'latest')
    ON CONFLICT (key) DO NOTHING;

-- Per-instance version tracking
ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS openclaw_version TEXT,
    ADD COLUMN IF NOT EXISTS target_openclaw_version TEXT,
    ADD COLUMN IF NOT EXISTS openclaw_update_status TEXT,
    ADD COLUMN IF NOT EXISTS openclaw_update_error TEXT;

-- +goose Down
ALTER TABLE vps_instances
    DROP COLUMN IF EXISTS openclaw_version,
    DROP COLUMN IF EXISTS target_openclaw_version,
    DROP COLUMN IF EXISTS openclaw_update_status,
    DROP COLUMN IF EXISTS openclaw_update_error;
DROP TABLE IF EXISTS platform_settings;
