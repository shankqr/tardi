-- +goose Up
-- Pin the platform-wide target Hermes version to v0.9.0 (tag v2026.4.13).
-- This is the first release with the built-in web dashboard that Tardi
-- exposes via the dashboard-shim auth gate. Existing instances pick this up
-- on their next heartbeat and run the Hermes install script for the new tag.
INSERT INTO platform_settings (key, value)
VALUES ('target_hermes_version', 'v2026.4.13')
ON CONFLICT (key) DO UPDATE
    SET value = EXCLUDED.value
    WHERE platform_settings.value IN ('main', 'latest', '');

-- +goose Down
DELETE FROM platform_settings WHERE key = 'target_hermes_version' AND value = 'v2026.4.13';
