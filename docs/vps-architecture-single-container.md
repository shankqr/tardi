# VPS Architecture (Single Container) — Diagrams, Flows & Connections

> **Status: PROPOSED** — Not yet implemented. See [Prerequisites](#prerequisites) at the bottom.
>
> This document mirrors [vps-architecture.md](vps-architecture.md) but describes the
> target single-container setup: OpenClaw on Docker host networking, Cloudflare Proxy
> for TLS, no Caddy.

## 1. VPS Container Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        VPS (Ubuntu 24.04)                           │
│                        Hetzner Cloud                                │
│                                                                     │
│  ┌──────────────── UFW Firewall ──────────────────────────────┐     │
│  │  ALLOW: 22/tcp (SSH), 80/tcp (HTTP)                        │     │
│  │  DENY:  everything else                                    │     │
│  │                                                            │     │
│  │  Note: No 443 needed — Cloudflare terminates TLS at edge.  │     │
│  │  Origin traffic arrives as plain HTTP on port 80.           │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌────────────── Docker: --network=host (no bridge) ──────────┐     │
│  │                                                             │    │
│  │  ┌────────────────────────────────────────────────────── ┐  │    │
│  │  │   openclaw-gateway                                    │  │    │
│  │  │   (ghcr.io/openclaw/openclaw:<tag>)                   │  │    │
│  │  │                                                       │  │    │
│  │  │   network_mode: host                                  │  │    │
│  │  │   Listens: 0.0.0.0:18789 (HTTP + WS + Control UI)    │  │    │
│  │  │                                                       │  │    │
│  │  │   Volumes:                                            │  │    │
│  │  │     ./data/openclaw → /home/node/.openclaw            │  │    │
│  │  │     /var/run/docker.sock (tool execution)             │  │    │
│  │  │                                                       │  │    │
│  │  │   User: 1000:1000                                     │  │    │
│  │  │   Healthcheck: curl localhost:18789/health (30s)      │  │    │
│  │  └────────────────────────────────────────────────────── ┘  │    │
│  │                                                             │    │
│  │  No Docker bridge network. No DNS resolution needed.        │    │
│  │  Container shares the host's network namespace directly.    │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  ┌──────────── iptables NAT ──────────────────────────────────┐     │
│  │  PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789  │     │
│  │                                                             │     │
│  │  Redirects port 80 → 18789 so Cloudflare can reach the     │     │
│  │  origin. OpenClaw can't bind port 80 (runs as UID 1000).   │     │
│  └─────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Systemd Services                                           │    │
│  │  ├── openclaw-stack.service (docker compose up -d)          │    │
│  │  ├── openclaw-heartbeat.timer (every 5 min)                 │    │
│  │  └── openclaw-heartbeat.service (runs heartbeat.sh)         │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  /opt/openclaw/                                                     │
│  ├── .env                    # All tokens & API keys                │
│  ├── .config_version         # Last synced config version           │
│  ├── docker-compose.yml      # Single service definition            │
│  ├── heartbeat.sh            # Heartbeat script (from backend)      │
│  └── data/openclaw/          # OpenClaw runtime data                │
│      └── openclaw.json       # Runtime config (OpenClaw-owned)      │
│                                                                     │
│  Removed vs. current architecture:                                  │
│  ✗ Caddyfile               (no longer needed)                       │
│  ✗ certs/                  (no self-signed TLS — Cloudflare edge)   │
│  ✗ caddy/{data,config}     (no Let's Encrypt cert persistence)      │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. Network Flow — External Traffic

```
                    Internet (User's browser)
                       │
                       │  https://<uuid>.tardi.ai
                       ▼
          ┌────────────────────────┐
          │  Cloudflare Proxy      │
          │  (orange cloud)        │
          │                        │
          │  • TLS termination     │
          │  • DDoS protection     │
          │  • WebSocket support   │
          │    (enabled by default │
          │     on free plan)      │
          │  • SSL mode: Flexible  │
          │    (CF→origin is HTTP) │
          └────────┬───────────────┘
                   │
                   │  HTTP (plain) to origin IP
                   │  Port 80
                   ▼
          ┌────────────────────────┐
          │  UFW Firewall          │
          │  allow 80, 22          │
          └────────┬───────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
   Port 80              Port 22
        │                     │
        ▼                     ▼
  ┌───────────┐         ┌─────────┐
  │ iptables  │         │  sshd   │
  │ NAT       │         │         │
  │ :80→:18789│         │ root pw │
  └─────┬─────┘         │  auth   │
        │               └─────────┘
        ▼
  ┌────────────────┐
  │ OpenClaw       │
  │ Gateway        │
  │ (host network) │
  │                │
  │ :18789         │
  │ WebSocket +    │
  │ HTTP + Control │
  │ UI (Lit SPA)   │
  └────────────────┘

  Key difference: No Caddy container in the path.
  Cloudflare handles TLS. iptables handles port mapping.
```

## 3. Authentication Flows

### 3a. Dashboard Access (Browser → OpenClaw Control UI)

```
Browser (User clicks "Open Agent Dashboard")
    │
    │  https://<uuid>.tardi.ai/#token=<OPENCLAW_AUTH_TOKEN>
    │
    ▼
┌──────────────┐                ┌──────────────┐
│  Cloudflare  │── HTTP :80 ──▶│  iptables    │
│  Proxy       │                │  :80 → :18789│
│  (TLS edge)  │                └──────┬───────┘
└──────────────┘                       │
                                       ▼
                               ┌──────────────┐
                               │ OpenClaw     │
                               │ Gateway      │
                               └──────┬───────┘
                                      │
                                      ▼
                              Serves Control UI
                              (Lit SPA HTML/JS)
                                      │
                                      ▼
                              JS reads #token
                              from URL hash
                                      │
                                      ▼
                              WebSocket connect:
                              wss://<uuid>.tardi.ai/
                              { auth: { token: "xxx" } }
                                      │
                              (Cloudflare upgrades
                               to WS, forwards to
                               origin :80 → :18789)
                                      │
                                      ▼
                              OpenClaw validates
                              against OPENCLAW_GATEWAY_TOKEN
                              env var
                                      │
                                      ▼
                              Device pairing SKIPPED
                              (dangerouslyDisableDeviceAuth: true)
                                      │
                                      ▼
                              ✅ Dashboard loads
```

### 3b. Backend RPC (Tardi Backend → OpenClaw)

```
Tardi Backend (Cloud Run)
    │
    │  ws://<IPv4>:18789/?token=<OPENCLAW_AUTH_TOKEN>
    │  (Plain WS — no TLS needed for server-to-server)
    │  (Bypasses Cloudflare entirely — uses IP directly)
    │
    ▼
┌──────────────┐
│ OpenClaw     │
│ Gateway      │
│ :18789       │
└──────┬───────┘
       │
       ▼
Token from URL query
string used for auth
       │
       ▼
RPC methods:
• config.get
• config.patch
• channels.status
• web.login.start

Note: Backend connects to VPS IP directly on port 18789,
NOT through Cloudflare. No TLS, no iptables NAT involved.
This is simpler than the Caddy setup where backend had to
use wss:// with InsecureSkipVerify for self-signed certs.
```

### 3c. Heartbeat (VPS → Backend)

```
heartbeat.sh (systemd timer, every 5 min)
    │
    │  POST {API_URL}/api/agent/heartbeat
    │  Authorization: Bearer {AGENT_TOKEN}
    │  Body: { status, openclaw_version, update_status }
    │
    ▼
┌──────────────────┐
│  Tardi Backend   │
│  (Cloud Run)     │
│                  │
│  Response:       │
│  { config_version│
│    target_ver }  │
└──────────────────┘

(Unchanged from current architecture)
```

### 3d. Internal Tool Calls (Agent → Gateway)

```
OpenClaw Agent (inside openclaw-gateway container)
    │
    │  ws://127.0.0.1:18789
    │  (host network — loopback goes directly to gateway)
    │
    ▼
OpenClaw Gateway (same container, same network namespace)
    │
    │  Auth: OPENCLAW_GATEWAY_TOKEN env var
    │
    ▼
Tool execution via Docker socket
    │
    ▼
Sandbox container spawned & destroyed

Note: Identical to current architecture. Host networking
does not change internal loopback behavior.
```

## 4. Token Map

```
┌──────────────────────┬──────────────────┬────────────────────────────┐
│ Token                │ Direction        │ Purpose                    │
├──────────────────────┼──────────────────┼────────────────────────────┤
│ AGENT_TOKEN          │ VPS → Backend    │ Heartbeat auth             │
│ (64-char hex)        │ HTTP Bearer      │                            │
├──────────────────────┼──────────────────┼────────────────────────────┤
│ OPENCLAW_AUTH_TOKEN  │ Frontend → VPS   │ Dashboard WebSocket auth   │
│ = OPENCLAW_GATEWAY_  │ URL hash #token= │ (same value, two names)    │
│   TOKEN              │                  │                            │
│ (64-char hex)        │ Backend → VPS    │ RPC WebSocket auth         │
│                      │ URL query ?token=│                            │
│                      │                  │                            │
│                      │ Internal loopback│ Agent tool call auth       │
│                      │ env var          │                            │
├──────────────────────┼──────────────────┼────────────────────────────┤
│ Root Password        │ Backend → VPS    │ SSH access for config sync │
│ (24-char hex)        │ SSH password     │ and management             │
└──────────────────────┴──────────────────┴────────────────────────────┘

(Token map is identical — Caddy removal doesn't affect token semantics.)
```

## 5. Config Change Flow (End-to-End)

```
User changes config in Dashboard
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│ Frontend                                                    │
│  1. PUT /api/instances/{id}/config  (save to DB, bump ver)  │
│  2. POST /api/instances/{id}/sync-config  (trigger SSH)     │
│  3. Poll GET /api/instances/{id}/sync-status every 5s       │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ Backend (Cloud Run)                                         │
│  1. Base64-encode config-sync.sh                            │
│  2. SSH into VPS as root                                    │
│  3. Decode script to /tmp/config-sync.sh                    │
│  4. systemd-run (detached, returns immediately)             │
└───────────────────────┬─────────────────────────────────────┘
                        │ SSH
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ VPS (config-sync.sh running via systemd-run)                │
│  1. Fetch config: GET /api/agent/config                     │
│  2. Rebuild .env with new keys/tokens                       │
│  3. docker compose up -d --force-recreate openclaw-gateway  │
│     (single container — no Caddy to worry about)            │
│  4. Wait for health (up to 60s)                             │
│  5. Apply post-startup config via CLI:                      │
│     • openclaw config set channels.telegram.streaming off   │
│     • openclaw config set channels.telegram.allowFrom [*]   │
│     • openclaw config set channels.telegram.dmPolicy open   │
│     • openclaw models set <model>                           │
│  6. Write .config_version (ONLY after all patches succeed)  │
│  7. Print "config sync complete" to journal                 │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ Frontend detects "complete" in poll response                │
│  → Shows success to user                                    │
│  → Calls POST /telegram/cleanup (RPC safety net)            │
│    → Backend sends config.patch via WebSocket RPC           │
└─────────────────────────────────────────────────────────────┘

Key difference: Step 3 only recreates one container (openclaw-gateway).
No Caddy container to restart, no cert state to preserve.
```

## 6. Heartbeat Loop (Every 5 Minutes)

```
systemd timer fires
         │
         ▼
    heartbeat.sh
         │
         ├──▶ curl localhost:18789/health → STATUS
         │
         ├──▶ POST /api/agent/heartbeat
         │    Response: { config_version, target_openclaw_version }
         │
         ├──▶ Config version mismatch?
         │    YES → Full config sync (fetch, rebuild .env,
         │          recreate container, apply patches)
         │
         ├──▶ Target OpenClaw version != current?
         │    YES → Pull new image → recreate → health check
         │          If unhealthy → ROLLBACK to old image
         │
         └──▶ Drift Guards (run every time):
              ├── Telegram: streaming=off, dmPolicy=open
              ├── Model: re-apply if missing
              ├── Auth: auth.mode=token
              ├── iptables: port 80 → 18789 NAT rule exists
              └── Control UI: dangerouslyDisableDeviceAuth=true

Removed drift guards (no longer needed):
  ✗ DNS stub listener fix (no Docker bridge = no container DNS issues)
  ✗ Caddyfile rewrite guard (no Caddyfile)
```

## 7. Provisioning Pipeline

```
User subscribes ($29/mo)
         │
         ▼
Backend creates provisioning job
         │
         ▼
┌────────────────────────────────────────────────────────┐
│ Step 1: SelectProvider (2 min)                         │
│   Verify Hetzner is available                          │
├────────────────────────────────────────────────────────┤
│ Step 2: CreateServer (5 min)                           │
│   Hetzner API → Ubuntu 24.04 + cloud-init user-data    │
│   Labels: instance_id, user_id, email                  │
│   DNS: create A record (<uuid>.tardi.ai, proxied=true) │
│   Preview DNS: <uuid>-b.tardi.ai (proxied=true)        │
│                                                        │
│   Note: DNS record created with Cloudflare Proxy ON    │
│   (orange cloud) from the start — no cert provisioning │
│   wait. Instant HTTPS via Cloudflare edge.             │
├────────────────────────────────────────────────────────┤
│ Step 3: WaitServerReady (5 min)                        │
│   Poll Hetzner API until status = "running"            │
├────────────────────────────────────────────────────────┤
│ Step 4: Bootstrap (10 min)                             │
│   Wait for cloud-init to complete                      │
│   Verify SSH connectivity                              │
│   cloud-init installs Docker, configures firewall,     │
│   creates users, writes configs, pulls images,         │
│   sets up iptables NAT (port 80 → 18789)              │
│                                                        │
│   No Caddy pull, no cert generation, no Caddyfile.     │
├────────────────────────────────────────────────────────┤
│ Step 5: InstallAgent (10 min)                          │
│   Wait for openclaw-gateway container to be healthy    │
│   Apply post-startup config (model, telegram)          │
├────────────────────────────────────────────────────────┤
│ Step 6: Activate (1 min)                               │
│   Mark instance as "active" in DB                      │
│   Start heartbeat timer                                │
└────────────────────────────────────────────────────────┘

Time saved: ~30-90s (no Caddy image pull, no Let's Encrypt
ACME challenge, no cert issuance wait).
```

## 8. Backend ↔ VPS Communication Summary

```
┌──────────────────┐                    ┌──────────────────┐
│  Tardi Backend   │                    │      VPS         │
│  (Cloud Run)     │                    │  (Hetzner)       │
│                  │                    │                  │
│                  │◀── HTTPS POST ─────│ Heartbeat        │
│                  │    :8080           │ (every 5 min)    │
│                  │    Bearer token    │                  │
│                  │                    │                  │
│                  │── SSH :22 ────────▶│ Config sync      │
│                  │   root + password  │ Script push      │
│                  │                    │ Status check     │
│                  │                    │                  │
│                  │── WS :18789 ──────▶│ RPC calls        │
│                  │   ?token=xxx       │ (config.patch,   │
│                  │   direct to IP     │  channels.status)│
│                  │   (no Caddy/TLS!)  │                  │
│                  │                    │                  │
│                  │── Hetzner API ─────│ Create/Delete    │
│                  │   (not to VPS)     │ Snapshot/Rebuild │
└──────────────────┘                    └──────────────────┘

Key change: Backend RPC uses plain ws:// on port 18789,
not wss:// through Caddy. No InsecureSkipVerify needed.
```

## 9. Cloudflare Proxy Configuration

```
┌─────────────────────────────────────────────────────────────┐
│ Cloudflare Zone: tardi.ai                                   │
│                                                             │
│ SSL/TLS mode: Flexible                                      │
│ (CF terminates TLS at edge, connects to origin via HTTP)    │
│                                                             │
│ WebSockets: Enabled (default on all plans)                  │
│                                                             │
│ DNS Records (per instance):                                 │
│ ┌───────────────────┬───────┬───────────────┬──────────┐    │
│ │ Name              │ Type  │ Content       │ Proxy    │    │
│ ├───────────────────┼───────┼───────────────┼──────────┤    │
│ │ <uuid>.tardi.ai   │ A     │ <vps-ip>      │ Proxied  │    │
│ │ <uuid>-b.tardi.ai │ A     │ <vps-ip>      │ Proxied  │    │
│ └───────────────────┴───────┴───────────────┴──────────┘    │
│                                                             │
│ Domain scheme: FLAT (single level subdomain)                │
│ Old: <uuid>.a.tardi.ai   (sub-subdomain, no free SSL)      │
│ New: <uuid>.tardi.ai     (covered by Universal SSL *.tardi) │
│                                                             │
│ Preview domain:                                             │
│ Old: <uuid>.b.tardi.ai                                      │
│ New: <uuid>-b.tardi.ai   (hyphen, not dot)                  │
└─────────────────────────────────────────────────────────────┘
```

## 10. docker-compose.yml (Single Container)

```yaml
services:
  openclaw-gateway:
    image: ghcr.io/openclaw/openclaw:<tag>
    container_name: openclaw-gateway
    restart: unless-stopped
    network_mode: host
    user: "1000:1000"
    group_add:
      - "${DOCKER_GID}"
    volumes:
      - ./data/openclaw:/home/node/.openclaw:rw
      - /var/run/docker.sock:/var/run/docker.sock
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:18789/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

# No 'caddy' service
# No 'networks' section
# No port mappings (host network exposes directly)
```

## 11. openclaw.json Changes

```json
{
  "gateway": {
    "bind": "lan",
    "controlUi": {
      "allowedOrigins": ["*"],
      "dangerouslyDisableDeviceAuth": true,
      "allowInsecureAuth": true
    },
    "auth": {
      "mode": "token"
    }
  }
}
```

Changes from current config:
- **Removed** `trustedProxies` — no reverse proxy in the path; Cloudflare
  connects directly. OpenClaw sees the real client IP (or Cloudflare edge IP).
  `CF-Connecting-IP` header carries the real client IP if needed.
- Everything else stays the same.

## 12. iptables NAT Rule

```bash
# Applied during cloud-init and verified by heartbeat drift guard.
# Persisted via iptables-persistent (installed in cloud-init).
#
# Why: OpenClaw runs as UID 1000, cannot bind ports below 1024.
# Cloudflare Proxy connects to origin on port 80 (Flexible SSL mode).
# This rule redirects incoming port 80 to OpenClaw's port 18789.

iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789

# Persist across reboots
apt-get install -y -qq iptables-persistent
netfilter-persistent save
```

## What Changes vs. Current Architecture

| Aspect | Current (2 containers) | Single container |
|--------|----------------------|------------------|
| Containers | openclaw-gateway + openclaw-caddy | openclaw-gateway only |
| Docker networking | Bridge (openclaw-net) | Host (--network=host) |
| TLS termination | Caddy (Let's Encrypt or self-signed) | Cloudflare Proxy (edge) |
| Port 80/443 | Caddy container | iptables NAT → 18789 |
| Domain scheme | `<uuid>.a.tardi.ai` (sub-subdomain) | `<uuid>.tardi.ai` (flat) |
| Preview domain | `<uuid>.b.tardi.ai` | `<uuid>-b.tardi.ai` |
| Backend RPC | `wss://<ip>` through Caddy (InsecureSkipVerify) | `ws://<ip>:18789` direct (plain) |
| Cert management | Caddy auto-renewal + persistence volumes | None (Cloudflare handles it) |
| DNS stub fix | Required (Docker bridge DNS issue) | Not needed (host network) |
| Caddyfile drift guard | Required (heartbeat checks config) | Not needed |
| Self-signed fallback | Yes (IP-based instances) | Not needed (Cloudflare edge cert) |
| Container startup order | Caddy depends_on gateway healthy | Single container, no ordering |
| DDoS protection | None (direct IP exposure) | Cloudflare (free tier) |
| IP-only access (no domain) | Works via self-signed cert | Not supported (Cloudflare requires domain) |

## What Gets Removed from Codebase

```
Backend:
  - Caddyfile template in provisioner.go
  - Caddy service in docker-compose template
  - Docker bridge network setup
  - Caddy cert volume setup
  - Self-signed cert generation (IP-based)
  - DNS stub listener fix
  - Caddyfile drift guard in heartbeat.sh
  - InsecureSkipVerify on backend RPC WebSocket dial
  - Caddy reload commands in sync scripts

Frontend:
  - (none — URL construction stays the same, just domain format changes)
```

## What Gets Added

```
Backend:
  - iptables NAT rule in cloud-init
  - iptables drift guard in heartbeat.sh
  - iptables-persistent install in cloud-init
  - Cloudflare DNS records created with proxied=true
  - Flat domain scheme (<uuid>.tardi.ai)
```

## Prerequisites

Before implementing this architecture, the following must be resolved:

1. **Flatten domain scheme** — Change from `<uuid>.a.tardi.ai` to `<uuid>.tardi.ai`.
   Cloudflare's free Universal SSL only covers `*.tardi.ai` and `tardi.ai`, NOT
   `*.a.tardi.ai` (sub-subdomains). This requires updating:
   - `CLOUDFLARE_BASE_DOMAIN` secret (from `a.tardi.ai` to `tardi.ai`)
   - Preview domain derivation logic (from `b.tardi.ai` to `<uuid>-b.tardi.ai`)
   - DNS record creation to set `proxied: true`
   - Existing active instance DNS migration

2. **Cloudflare SSL mode** — Set zone SSL mode to **Flexible** (CF→origin is HTTP).
   Requires Cloudflare dashboard access or API token with SSL permissions (the current
   DNS-scoped token cannot change this).

3. **Cloudflare WebSocket** — Verify WebSocket connections work through Cloudflare Proxy
   on the free plan (enabled by default, but must test with OpenClaw's specific
   connect/challenge protocol and long-lived dashboard sessions).

4. **Port 80 origin** — Confirm Cloudflare Proxy connects to origin port 80 with
   Flexible SSL mode. This is standard behavior but should be validated.

5. **No IP-only fallback** — Instances without a domain currently fall back to self-signed
   certs via Caddy. The single-container setup requires a domain (Cloudflare Proxy
   needs DNS). Decide: is IP-only access still needed? If yes, keep a Caddy fallback
   path or use `allowInsecureAuth: true` with direct HTTP.

## Key Source Files (to modify)
- `backend/internal/jobs/provisioner.go` — Cloud-init template, docker-compose, remove Caddy
- `backend/internal/scripts/heartbeat.go` — Remove Caddy guards, add iptables guard
- `backend/internal/api/sync.go` — Config sync: remove Caddy restart, simplify
- `backend/internal/api/whatsapp.go` — Change wss:// to ws://:18789 (no TLS)
- `backend/internal/dns/cloudflare.go` — Create records with `proxied: true`
- `backend/internal/config/config.go` — Update base domain handling
