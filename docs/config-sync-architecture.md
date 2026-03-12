# Config Sync Architecture

## Overview

Real-time configuration propagation from the dashboard frontend to the VPS-hosted agent, with live progress feedback.

```
Frontend (Save)
  → POST /api/instances/{id}/config        (save to DB)
  → POST /api/instances/{id}/sync-config   (trigger SSH)
  → Poll GET /api/instances/{id}/sync-status every 5s
       ↓
Backend (Cloud Run)
  → SSH into VPS
  → systemd-run --no-block bash /tmp/config-sync.sh
       ↓
VPS (systemd transient unit: tardi-config-sync)
  → Fetch config from API
  → Rebuild .env with new keys/tokens
  → Update openclaw.json (telegram channel enable/disable)
  → docker compose up -d --force-recreate
  → Wait for health check
  → Set default AI model
  → Write config version file
       ↓
Frontend polls sync-status
  → Backend SSHs in, queries systemctl + journalctl
  → Returns running/completed/failed
  → Frontend shows green "Configuration applied successfully"
```

## Key Technical Decisions

### systemd-run vs nohup/setsid

Go's SSH `session.Run()` waits for **all IO pipes to close**, not just the main process to exit. This means:

- `nohup cmd &` — fails because inherited FDs keep SSH channel open
- `setsid cmd </dev/null >/file 2>&1 &` — still waits ~8s for a 5s script
- **`systemd-run --no-block --collect`** — returns immediately, fully decoupled

systemd-run creates a transient service unit that:
- Runs independently of the SSH session
- Captures stdout/stderr in the journal
- Tracks ActiveState (active/inactive/failed)
- Auto-cleans up with `--collect`

### journalctl vs log files

`systemd-run` sends output to the systemd journal, not a log file. The status endpoint queries `journalctl -u tardi-config-sync -n 1 -o cat` for the last line of output.

### Polling vs WebSocket

5-second polling interval with 120-second timeout:
- Stateless: no persistent connection needed through Cloud Run
- Fault-tolerant: missing a poll doesn't break anything
- Simple: ~60-90s operation doesn't need sub-second latency
- Graceful degradation: if frontend gives up, heartbeat applies config within 5 minutes

## Frontend State Machine

```
idle → saving → syncing → success (auto-dismiss 8s)
                        → failed  (retry or dismiss)
```

Progress messages during `syncing`:
- 0-10s: "Connecting to your agent"
- 10-30s: "Updating configuration"
- 30-60s: "Restarting with new settings"
- 60s+: "Waiting for health check"
- 120s+: timeout → "Sync is taking longer than expected"

## Config Sync Script Flow

1. Extract `API_URL` and `AGENT_TOKEN` from `.env` (grep+cut, not source)
2. Fetch config JSON from `/api/agent/config`
3. Parse keys: openrouter, anthropic, openai, telegram, provider, model
4. Rebuild `.env` atomically (write .tmp → mv)
5. Update `openclaw.json` telegram channel (jq)
6. `docker compose up -d --force-recreate openclaw-gateway`
7. Poll health endpoint up to 60s, then set default model
8. Write config version to `/opt/openclaw/.config_version`

## Fallback

If instant sync fails, the heartbeat script (runs every 5 minutes via systemd timer on VPS) compares config versions and applies any pending changes automatically.

## Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/instances/{id}/config` | PUT | Save config to DB |
| `/api/instances/{id}/sync-config` | POST | Trigger SSH sync on VPS |
| `/api/instances/{id}/sync-status` | GET | Poll systemd unit state via SSH |

## Applies To

- AI Provider (OpenRouter API key, model, provider)
- Telegram bot token (connect/disconnect)
- Any future config that needs instant propagation to VPS
