-- +goose Up

-- Global platform settings (single-row key-value store)
CREATE TABLE platform_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO platform_settings (key, value) VALUES ('target_openclaw_version', 'latest');

-- Per-instance version tracking
ALTER TABLE vps_instances
    ADD COLUMN openclaw_version TEXT,               -- currently running version (reported by heartbeat)
    ADD COLUMN target_openclaw_version TEXT,         -- per-instance override (NULL = use global)
    ADD COLUMN openclaw_update_status TEXT,          -- pulling/updating/failed/completed
    ADD COLUMN openclaw_update_error TEXT;           -- error message if update failed

-- +goose Down
ALTER TABLE vps_instances
    DROP COLUMN IF EXISTS openclaw_version,
    DROP COLUMN IF EXISTS target_openclaw_version,
    DROP COLUMN IF EXISTS openclaw_update_status,
    DROP COLUMN IF EXISTS openclaw_update_error;
DROP TABLE IF EXISTS platform_settings;
