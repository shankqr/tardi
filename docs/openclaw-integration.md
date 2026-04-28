# OpenClaw Integration Guide

OpenClaw is the AI agent runtime that runs on each user's VPS inside a Docker container.

## Key Behaviors

- **OpenClaw owns `openclaw.json`** — it overwrites the file on startup with its internal config. Do NOT rely on editing this file externally; changes will be lost.
- **Config changes** must go through OpenClaw's `config.patch` WebSocket RPC (see `whatsapp.go` for examples), or be set before the very first boot.
- **`config.patch` format**: Requires `{raw: "<JSON string of patch>", baseHash: "<hash from config.get>"}`. NOT `hash` (rejected as unexpected property), NOT direct config as params (rejected as missing `raw`). Always call `config.get` first to obtain the `baseHash`.
- **Telegram bot token** is passed via `TELEGRAM_BOT_TOKEN` env var. OpenClaw auto-detects it and configures the Telegram channel automatically.

## Architecture

Single-container setup with Cloudflare for TLS:

```
Browser (user)
    │
    │  HTTPS
    ▼
Cloudflare (TLS termination)
    │
    │  HTTP
    ▼
Caddy (host binary, port 80)
    │  hostname-based routing
    ├─ preview domain → localhost:3000 (user-built apps)
    └─ everything else → localhost:18789 (OpenClaw gateway)
              │
              ▼
OpenClaw Gateway (Docker container, port 18789)
    auth.mode: "token"
```

- **No self-signed certs** — Cloudflare handles TLS at the edge
- **Caddy runs as a host binary** (not a Docker container) — simple HTTP reverse proxy
- **OpenClaw is the only Docker container** (plus sandbox image for code execution)

## Host Admin Helper

OpenClaw still runs as UID 1000 inside the `openclaw-gateway` container, but it
gets an intentional root bridge to the VPS host. Host-level tasks go through a
root-owned helper installed as `tardi-host-admin.service`.

The helper listens on a Unix socket at `/run/tardi-host-admin/admin.sock`. The
socket directory and a small client are mounted into OpenClaw:

```yaml
volumes:
  - /run/tardi-host-admin:/run/tardi-host-admin:rw
  - /opt/openclaw/host-admin/bin:/opt/tardi/bin:ro
env:
  TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock
```

From inside OpenClaw, the agent can run:

```bash
/opt/tardi/bin/sudo apt-get update
/opt/tardi/bin/sudo apt-get install -y ffmpeg build-essential
/opt/tardi/bin/sudo systemctl restart caddy
/opt/tardi/bin/tardi-host-admin host.exec 'git clone https://github.com/example/repo /opt/work/repo'
/opt/tardi/bin/tardi-host-admin desktop.status
/opt/tardi/bin/tardi-host-admin desktop.install
/opt/tardi/bin/tardi-host-admin desktop.start
/opt/tardi/bin/tardi-host-admin desktop.open BINANCE:BTCUSDT
```

`/opt/tardi/bin` is prepended to `PATH`, so `sudo <command>` resolves to the
Tardi shim inside the container. The shim forwards commands to `host.exec`,
which runs `/bin/bash -lc <command>` as root on the VPS host.

| Action | Root-side behavior |
|---|---|
| `desktop.status` | Report helper, VNC/XFCE, and TradingView status |
| `desktop.install` | Install XFCE, TigerVNC, X11 tools, and TradingView Desktop from TradingView's official Debian repo |
| `desktop.start` / `desktop.stop` / `desktop.restart` | Manage the private `tardi-desktop.service` X11 VNC session |
| `desktop.open` | Start the desktop session and launch TradingView for a symbol |
| `host.exec` | Run an arbitrary shell command as root on the VPS host |

The `host.exec` bridge is intentionally broad: package installs, framework
setup, Docker commands, GitHub clones, service management, and file writes all
run against the host rather than the OpenClaw container filesystem.

## Gateway Auth — Token Mode

The gateway uses `auth.mode: "token"` with `OPENCLAW_GATEWAY_TOKEN` env var. This was chosen over two alternatives that don't work:

- `auth.mode: "none"` — **crashes**: OpenClaw refuses to start with "Refusing to bind gateway to lan without auth" when `bind: "lan"` is set
- `auth.mode: "trusted-proxy"` — **breaks internal tool calls**: requires `X-Forwarded-User` header from a reverse proxy, but when the agent calls tools internally (sessions_list, browser, etc.) the calls go directly to `ws://127.0.0.1:18789` bypassing Caddy — no header, no auth, unauthorized

**Two tokens, same value:**
- `OPENCLAW_AUTH_TOKEN` — stored in DB, sent to frontend for building the dashboard URL
- `OPENCLAW_GATEWAY_TOKEN` — read by OpenClaw for gateway token auth. Same value as `OPENCLAW_AUTH_TOKEN`

### Config in `openclaw.json`

```json
{
  "gateway": {
    "bind": "lan",
    "trustedProxies": ["0.0.0.0/0"],
    "controlUi": {
      "allowedOrigins": ["*"],
      "dangerouslyDisableDeviceAuth": true,
      "allowInsecureAuth": true
    },
    "auth": { "mode": "token" }
  }
}
```

| Key | Purpose |
|-----|---------|
| `bind: "lan"` | Binds to `0.0.0.0` so Caddy can reach it. Without this, only listens on loopback. |
| `trustedProxies: ["0.0.0.0/0"]` | Tells OpenClaw to trust proxy headers from Cloudflare. Without this, "untrusted proxy" errors block operator scopes. Safe because auth is enforced via token. |
| `allowedOrigins: ["*"]` | Allows Control UI to load from any origin (browser sees the domain via Cloudflare). |
| `dangerouslyDisableDeviceAuth: true` | Disables device pairing — any browser with the correct token can connect. |
| `allowInsecureAuth: true` | Required for shared token auth to grant operator scopes (OC 2026.3.22+). |
| `auth.mode: "token"` | Authenticates all connections via `OPENCLAW_GATEWAY_TOKEN` env var. |

## Control UI Dashboard Auth

The Control UI is a Lit-based SPA served by OpenClaw's gateway.

**Token delivery via URL hash fragment:**
The frontend opens the dashboard as `https://<domain>/#token=<OPENCLAW_AUTH_TOKEN>`. The Control UI JS reads the token from the hash fragment and includes it in the WebSocket `connect` message's `auth.token` field. This is the ONLY way to deliver the token — query params, headers, and cookies don't work.

**Device pairing disabled:**
`dangerouslyDisableDeviceAuth: true` in `openclaw.json` allows any browser with the correct token to connect without pairing.

### Auth Flows

```
Browser (user clicks "Open Agent Dashboard"):
  1. Frontend opens https://<domain>/#token=<OPENCLAW_AUTH_TOKEN>
  2. Cloudflare terminates TLS, forwards HTTP to Caddy on port 80
  3. Caddy proxies to OpenClaw on localhost:18789
  4. OpenClaw serves the Control UI HTML/JS
  5. Control UI JS reads token from URL hash fragment (#token=xxx)
  6. JS creates WebSocket to wss://<domain>/
  7. JS sends connect message with auth.token = the hash token
  8. OpenClaw validates token against OPENCLAW_GATEWAY_TOKEN env var
  9. Device pairing skipped (dangerouslyDisableDeviceAuth: true)
  10. Connected — dashboard loads

Backend RPC (openclawRPC in whatsapp.go):
  1. Backend connects to ws://<ip>:18789/?token=<token> (direct, no Cloudflare)
  2. Sends connect message with auth.token
  3. OpenClaw validates token
  4. Connected — RPC calls proceed

Internal tool calls (agent → gateway):
  1. Agent calls ws://127.0.0.1:18789 for tool execution
  2. OpenClaw authenticates using OPENCLAW_GATEWAY_TOKEN env var
```

### What Does NOT Work for Control UI Auth (Tried and Failed)

- `?token=xxx` in URL query string — JS deletes it from URL but does NOT use it for auth
- Caddy `rewrite` to inject `?token=xxx` — only affects HTTP-level auth, JS still sends empty connect message
- Caddy `header_up Authorization "Bearer xxx"` — OpenClaw ignores this header for WebSocket auth
- `#gatewayUrl=wss://domain/?token=xxx` — treated as "pending" URL requiring user confirmation
- `#password=xxx` — not reliably picked up by the JS WebSocket client
- `auth.mode: "trusted-proxy"` with `X-Forwarded-User` — breaks internal tool calls

### Caddy Configuration

Caddy runs as a host binary (not Docker), listening on port 80. Cloudflare handles TLS at the edge.

```
{PreviewDomain} {
    reverse_proxy localhost:3000
}

http:// {
    reverse_proxy localhost:18789
}
```

Custom Caddyfiles can be set per-instance via the `custom_caddyfile` DB field. The heartbeat drift guard syncs the Caddyfile from the API and replaces it if changed.

### Drift Guard

`heartbeat.go` runs every 5 minutes and ensures:
- `auth.mode: "token"` in `openclaw.json`
- `allowInsecureAuth: true` is set
- `trustedProxies: ["0.0.0.0/0"]` is set
- `dangerouslyDisableDeviceAuth: true` is set
- `OPENCLAW_GATEWAY_TOKEN` matches the running container's token
- Caddyfile matches expected config (or custom Caddyfile from API)

If the container is crash-looping because OpenClaw reverted auth.mode to `"none"`, the drift guard fixes `openclaw.json` on disk and force-recreates the container.

## Telegram Config

### Problem: Bad Defaults

When OpenClaw auto-detects `TELEGRAM_BOT_TOKEN` from the env var, it creates a Telegram channel with bad defaults:

1. **Double replies**: Default `streaming: "partial"` sends an initial streaming chunk as one message, then the full response as a second message.
2. **Pairing prompt required**: Default `dmPolicy` is not `"open"`, so users see "access not configured" and must manually run a pairing command.

### Required Telegram Channel Settings

Applied post-startup via CLI or config.patch RPC:

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

Account-level overrides must also be fixed (OpenClaw auto-creates per-account entries with bad defaults).

### Three Layers of Protection

1. **Cloud-init** (`provisioner.go`): After container health check, runs `openclaw config set` CLI commands for each setting.
2. **Config.patch RPC** (`telegram.go` — `patchTelegramConfig`): Called by frontend after sync completes. Atomic (no restart, <1s) — preferred over CLI (which triggers gateway restart per command, ~4-6s each).
3. **Heartbeat drift guard** (`heartbeat.go`): Checks every 5 minutes. Reads `openclaw.json`, fixes if streaming or dmPolicy drifted. Guards against Docker auto-restarts resetting config.

### Config Sync Flow

The sync script (`backend/internal/api/sync.go`) delegates Telegram config to the config.patch RPC rather than running CLI commands itself (avoids 20-30s downtime from 5 sequential CLI restarts). The frontend calls `POST /telegram/cleanup` after sync completes, which triggers `patchTelegramConfig()`.

### Important Notes

- Do NOT include `channels.telegram` in the cloud-init `openclaw.json` template — OpenClaw auto-detects from `TELEGRAM_BOT_TOKEN` env var
- `dmPolicy: "open"` requires `allowFrom: ["*"]` — omitting `allowFrom` causes a config validation error and crash loop
- `allowFrom` must be set BEFORE `dmPolicy` when using sequential CLI commands (validation order dependency)
- The `config.patch` RPC uses WebSocket protocol (see `openclawRPC` in `whatsapp.go`)

## Debugging OpenClaw on VPS

```bash
# SSH into VPS
ssh -i ~/.ssh/tardi-backend root@<ip>

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

# Check heartbeat status
systemctl status openclaw-heartbeat.timer
journalctl -u openclaw-heartbeat.service --no-pager -n 50
```
