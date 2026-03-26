# OpenClaw Integration Guide

OpenClaw is the AI agent runtime that runs on each user's VPS inside a Docker container.

## Key Behaviors

- **OpenClaw owns `openclaw.json`** — it overwrites the file on startup with its internal config. Do NOT rely on editing this file externally; changes will be lost.
- **Config changes** must go through OpenClaw's `config.patch` WebSocket RPC (see `whatsapp.go` for examples), or be set before the very first boot.
- **`config.patch` format**: Requires `{raw: "<JSON string of patch>", baseHash: "<hash from config.get>"}`. NOT `hash` (rejected as unexpected property), NOT direct config as params (rejected as missing `raw`). Always call `config.get` first to obtain the `baseHash`.
- **Telegram bot token** is passed via `TELEGRAM_BOT_TOKEN` env var. OpenClaw auto-detects it and configures the Telegram channel automatically.

## Gateway Auth — Token Mode

The gateway uses `auth.mode: "token"` with `OPENCLAW_GATEWAY_TOKEN` env var. This was chosen over two alternatives that don't work:

- `auth.mode: "none"` — **crashes**: OpenClaw refuses to start with "Refusing to bind gateway to lan without auth" when `bind: "lan"` is set
- `auth.mode: "trusted-proxy"` — **breaks internal tool calls**: requires `X-Forwarded-User` header from a reverse proxy, but when the agent calls tools internally (sessions_list, browser, etc.) the calls go directly to `ws://127.0.0.1:18789` bypassing Caddy — no header, no auth, unauthorized

**Two tokens, same value:**
- `OPENCLAW_AUTH_TOKEN` — stored in DB, sent to frontend for building the dashboard URL
- `OPENCLAW_GATEWAY_TOKEN` — read by OpenClaw for gateway token auth. Same value as `OPENCLAW_AUTH_TOKEN`

## Control UI Dashboard Auth

The Control UI is a Lit-based SPA served by OpenClaw's gateway. It authenticates via a two-layer mechanism:

**Layer 1 — Token delivery via URL hash fragment:**
The frontend opens the dashboard as `https://<domain>/#token=<OPENCLAW_AUTH_TOKEN>`. The Control UI JS reads the token from the hash fragment (NOT query string, NOT HTTP headers) and includes it in the WebSocket `connect` message's `auth.token` field. This is the ONLY way to deliver the token to the Control UI — Caddy headers, URL rewrites, and query params do NOT work because the JS ignores them.

**Layer 2 — Device pairing disabled:**
OpenClaw normally requires "device pairing" for each new browser (crypto-based device identity + admin approval). This is disabled via `gateway.controlUi.dangerouslyDisableDeviceAuth: true` in `openclaw.json`, allowing any browser with the correct token to connect without pairing.

### Full Auth Flow

```
Browser (user clicks "Open Agent Dashboard"):
  1. Frontend opens https://<domain>/#token=<OPENCLAW_AUTH_TOKEN>
  2. Caddy is a transparent reverse proxy (TLS termination only, no auth logic)
  3. OpenClaw serves the Control UI HTML/JS
  4. Control UI JS reads token from URL hash fragment (#token=xxx)
  5. JS creates WebSocket to wss://<domain>/ (through Caddy)
  6. JS sends connect message with auth.token = the hash token
  7. OpenClaw validates token against OPENCLAW_GATEWAY_TOKEN env var
  8. Device pairing skipped (dangerouslyDisableDeviceAuth: true)
  9. Connected — dashboard loads

Backend RPC (openclawRPC in whatsapp.go):
  1. Backend connects to wss://<ip>/?token=<token> (token in URL)
  2. Sends connect message WITHOUT auth field
  3. OpenClaw uses HTTP-level token from URL — no pairing needed
  4. Connected — RPC calls proceed

Internal tool calls (agent → gateway):
  1. Agent calls ws://127.0.0.1:18789 for tool execution
  2. OpenClaw authenticates using OPENCLAW_GATEWAY_TOKEN env var
```

### Config in `openclaw.json`

```json
{
  "gateway": {
    "bind": "lan",
    "controlUi": {
      "allowedOrigins": ["*"],
      "dangerouslyDisableDeviceAuth": true
    },
    "auth": { "mode": "token" }
  }
}
```

### What Does NOT Work for Control UI Auth (Tried and Failed)

- `?token=xxx` in URL query string — JS deletes it from URL but does NOT use it for auth
- Caddy `rewrite` to inject `?token=xxx` — only affects HTTP-level auth, JS still sends empty connect message
- Caddy `header_up Authorization "Bearer xxx"` — OpenClaw ignores this header for WebSocket auth
- `#gatewayUrl=wss://domain/?token=xxx` — treated as "pending" URL requiring user confirmation
- `#password=xxx` — not reliably picked up by the JS WebSocket client
- `auth.mode: "trusted-proxy"` with `X-Forwarded-User` — breaks internal tool calls

### Caddy Configuration

Simple transparent proxy — no auth headers, no rewrites, no cookies. Caddy only does TLS termination and proxying.

```
<domain> {
    reverse_proxy openclaw-gateway:18789
}
```

### Drift Guard

`heartbeat.go` runs every 5 minutes and ensures:
- `auth.mode: "token"` in `openclaw.json`
- `OPENCLAW_GATEWAY_TOKEN` is in `.env`
- `dangerouslyDisableDeviceAuth: true` is set
- Caddyfile is a clean transparent proxy (removes old auth patterns)

## Telegram Config

### Problem: Bad Defaults

When OpenClaw auto-detects `TELEGRAM_BOT_TOKEN` from the env var, it creates a Telegram channel with bad defaults:

1. **Double replies**: Default `streaming: "partial"` sends an initial streaming chunk as one message, then the full response as a second message.
2. **Pairing prompt required**: Default `dmPolicy` is not `"open"`, so users see "access not configured" and must manually run a pairing command.

### Required Telegram Channel Settings

Applied post-startup via CLI or RPC:

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "dmPolicy": "open",
      "allowFrom": ["*"],
      "groupPolicy": "disabled",
      "streaming": "off"
    }
  }
}
```

### Config Sync Flow

The config is applied through two mechanisms:

1. **SSH sync script** (`backend/internal/api/sync.go` — `configSyncScript`): Triggered when user enters bot token in dashboard. Recreates the container, waits for health, then applies config via `openclaw config set` CLI commands.
2. **Cleanup RPC** (`backend/internal/api/telegram.go` — `patchTelegramConfig`): Called by frontend after sync completes. Uses WebSocket `config.patch` RPC as a safety net.

**Critical ordering**: The sync script MUST print `"config sync complete"` only AFTER the Telegram config CLI commands have run. The frontend polls for this message to detect completion.

**Previous bug (fixed 2026-03-17)**: The sync script printed completion BEFORE waiting for health and applying config. The frontend detected early completion, called the cleanup RPC (which failed since the container wasn't ready), and showed success. The user saw "Telegram bot connected" but the bot still required pairing because `dmPolicy:"open"` was never applied.

### Important Notes

- Do NOT include `channels.telegram` in the cloud-init `openclaw.json` template — OpenClaw auto-detects from `TELEGRAM_BOT_TOKEN` env var
- `dmPolicy: "open"` requires `allowFrom: ["*"]` — omitting `allowFrom` causes a config validation error and crash loop
- `allowFrom` must be set BEFORE `dmPolicy` when using sequential CLI commands (validation order dependency)
- The `config.patch` RPC uses WebSocket protocol (see `openclawRPC` in `whatsapp.go`)
- The heartbeat script (`provisioner.go`) has its own config sync section that correctly applies config before writing the version file

## Debugging OpenClaw on VPS

```bash
# SSH into VPS
ssh root@<ip>

# Check current config (OpenClaw's actual runtime config)
cat /opt/openclaw/data/openclaw/openclaw.json | jq .

# Container logs
docker logs openclaw-gateway 2>&1

# Detailed log file (inside container)
docker exec openclaw-gateway cat /tmp/openclaw/openclaw-$(date +%Y-%m-%d).log

# Check Telegram webhook status (should be empty URL for polling)
TG_TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" /opt/openclaw/.env | cut -d= -f2-)
curl -sf "https://api.telegram.org/bot${TG_TOKEN}/getWebhookInfo" | jq .

# Data directory structure
ls -la /opt/openclaw/data/openclaw/
```
