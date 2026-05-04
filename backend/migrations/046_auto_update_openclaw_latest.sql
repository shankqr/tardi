-- +goose Up
-- Track ghcr.io/openclaw/openclaw:latest so VPSes update automatically as soon
-- as the heartbeat observes a new image digest.
INSERT INTO platform_settings (key, value)
VALUES ('target_openclaw_version', 'latest')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();

-- Per-instance pins override the global auto-update policy. Clear existing
-- OpenClaw pins so all users move with the platform target.
UPDATE vps_instances
SET target_openclaw_version = NULL,
    updated_at = now()
WHERE framework = 'openclaw'
  AND target_openclaw_version IS NOT NULL;

-- +goose Down
INSERT INTO platform_settings (key, value)
VALUES ('target_openclaw_version', '2026.5.2')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();
