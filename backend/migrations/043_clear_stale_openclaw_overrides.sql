-- +goose Up

-- Migration 042 pinned the global OpenClaw target to 2026.4.26, but existing
-- per-instance overrides still take precedence in heartbeat responses. Clear
-- stale overrides that would keep instances on the broken 2026.4.24 tag.
UPDATE vps_instances
SET target_openclaw_version = NULL,
    updated_at = now()
WHERE framework = 'openclaw'
  AND target_openclaw_version IN ('latest', 'main', '2026.4.24');

-- +goose Down

-- Intentionally irreversible: these were stale operational overrides.
