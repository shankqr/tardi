-- +goose Up
-- Track the official Hermes Docker image's latest tag. Hermes heartbeat pulls
-- every cycle and recreates the container only when the image digest changes.
INSERT INTO platform_settings (key, value)
VALUES ('target_hermes_version', 'latest')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();

-- Per-instance pins override the global auto-update policy. Clear existing
-- Hermes pins so all hosted Hermes agents move with the platform target.
UPDATE vps_instances
SET target_openclaw_version = NULL,
    updated_at = now()
WHERE framework = 'hermes'
  AND target_openclaw_version IS NOT NULL;

-- +goose Down
INSERT INTO platform_settings (key, value)
VALUES ('target_hermes_version', 'v2026.4.13')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();
