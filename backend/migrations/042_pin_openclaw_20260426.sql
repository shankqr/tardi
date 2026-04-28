-- +goose Up

-- Pin OpenClaw to the latest deployable GHCR image tag as of 2026-04-28.
-- GitHub release tags include a leading "v", but Docker image tags do not.
INSERT INTO platform_settings (key, value)
VALUES ('target_openclaw_version', '2026.4.26')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();

-- +goose Down

UPDATE platform_settings
SET value = 'latest',
    updated_at = now()
WHERE key = 'target_openclaw_version'
  AND value = '2026.4.26';
