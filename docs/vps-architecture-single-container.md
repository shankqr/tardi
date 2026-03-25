# VPS Architecture (Single Container + Caddy) — Diagrams, Flows & Connections

> **Status: IMPLEMENTED** — Deployed to dev and prod. Existing instances are migrated automatically via heartbeat.
>
> OpenClaw on Docker host networking, Cloudflare Proxy for TLS, Caddy for
> hostname-based routing (dashboard vs. preview app).

## 1. VPS Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        VPS (Ubuntu 24.04)                           │
│                        Hetzner Cloud                                │
│                                                                     │
│  ┌──────────────── UFW Firewall ──────────────────────────────┐     │
│  │  ALLOW: 80/tcp (HTTP, any source)                          │     │
│  │  ALLOW: 18789 from Cloudflare IPs + backend egress CIDRs   │     │
│  │  ALLOW: 22/tcp from backend egress CIDRs (or any if unset) │     │
│  │  DENY:  everything else                                    │     │
│  │                                                            │     │
│  │  Port 18789: restricted to Cloudflare IPs + backend CIDRs  │     │
│  │  to block direct-IP bypass of Cloudflare proxy/WAF.        │     │
│  │  Cloudflare IPs refreshed daily by heartbeat.              │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌──────────── Caddy (systemd, host binary) ─────────────────┐     │
│  │                                                            │     │
│  │  /usr/local/bin/caddy — static binary, no Docker           │     │
│  │  Listens: 0.0.0.0:80 (HTTP only, no TLS)                  │     │
│  │  Config: /etc/caddy/Caddyfile                              │     │
│  │                                                            │     │
│  │  Routing (by Host header):                                 │     │
│  │  ┌──────────────────────────────────────────────────┐      │     │
│  │  │  http://<uuid>-b.tardi.ai → localhost:3000       │      │     │
│  │  │  (preview domain → user-built apps)              │      │     │
│  │  ├──────────────────────────────────────────────────┤      │     │
│  │  │  http:// (catch-all) → localhost:18789           │      │     │
│  │  │  (main domain → OpenClaw gateway)                │      │     │
│  │  └──────────────────────────────────────────────────┘      │     │
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
│  ┌──────────── User App (spawned by agent) ──────────────────┐     │
│  │  Port 3000 — web server started by the AI agent            │     │
│  │  (e.g., Node.js, Python, etc.)                             │     │
│  │  Accessible via preview domain through Caddy               │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Systemd Services                                           │    │
│  │  ├── caddy.service (Caddy reverse proxy on :80)             │    │
│  │  ├── openclaw-stack.service (docker compose up -d)          │    │
│  │  ├── openclaw-heartbeat.timer (every 5 min)                 │    │
│  │  └── openclaw-heartbeat.service (runs heartbeat.sh)         │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  /opt/openclaw/                                                     │
│  ├── .env                    # All tokens, API keys, PREVIEW_DOMAIN │
│  ├── .config_version         # Last synced config version           │
│  ├── docker-compose.yml      # Single service definition            │
│  ├── heartbeat.sh            # Heartbeat script (from backend)      │
│  └── data/openclaw/          # OpenClaw runtime data                │
│      └── openclaw.json       # Runtime config (OpenClaw-owned)      │
│                                                                     │
│  /etc/caddy/                                                        │
│  └── Caddyfile               # Hostname-based routing rules         │
│                                                                     │
│  /usr/local/bin/caddy        # Caddy static binary                  │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. Network Flow — External Traffic

```
                    Internet (User's browser)
                       │
          ┌────────────┴────────────┐
          │                         │
          │  https://<uuid>.tardi.ai         https://<uuid>-b.tardi.ai
          │  (dashboard)                     (preview app)
          │                         │
          └────────────┬────────────┘
                       ▼
          ┌────────────────────────┐
          │  Cloudflare Proxy      │
          │  (orange cloud)        │
          │                        │
          │  • TLS termination     │
          │  • DDoS protection     │
          │  • WebSocket support   │
          │  • SSL mode: Flexible  │
          │    (CF→origin is HTTP) │
          └────────┬───────────────┘
                   │
                   │  HTTP (plain) to origin IP
                   │  Port 80
                   ▼
          ┌────────────────────────┐
          │  UFW Firewall          │
          │  allow 80 (any)        │
          │  allow 18789 (CF+BE)   │
          │  allow 22 (BE CIDRs)   │
          └────────┬───────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
   Port 80              Port 22
        │                     │
        ▼                     ▼
  ┌───────────┐         ┌─────────┐
  │ Caddy     │         │  sshd   │
  │ (systemd) │         │         │
  │ :80       │         │ key auth│
  └─────┬─────┘         └─────────┘
        │
        │  Routes by Host header:
        │
        ├── Host: <uuid>-b.tardi.ai
        │         │
        │         ▼
        │   ┌────────────────┐
        │   │ User App       │
        │   │ localhost:3000  │
        │   │ (portfolio,    │
        │   │  todo app, etc)│
        │   └────────────────┘
        │
        └── Host: <uuid>.tardi.ai (or anything else)
                  │
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

  Caddy is a lightweight host-level reverse proxy (not Docker).
  No TLS — Cloudflare handles that. Caddy only does HTTP routing.
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
│  Cloudflare  │── HTTP :80 ──▶│  Caddy       │
│  Proxy       │                │  Host: <uuid>│
│  (TLS edge)  │                │  → :18789    │
└──────────────┘                └──────┬───────┘
                                       │
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
                               Caddy → :18789)
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

### 3b. Preview App Access (Browser → User-Built App)

```
Browser (User visits preview URL)
    │
    │  https://<uuid>-b.tardi.ai
    │
    ▼
┌──────────────┐                ┌──────────────┐
│  Cloudflare  │── HTTP :80 ──▶│  Caddy       │
│  Proxy       │                │  Host: *-b   │
│  (TLS edge)  │                │  → :3000     │
└──────────────┘                └──────┬───────┘
                                       │
                                       ▼
                               ┌──────────────┐
                               │ User App     │
                               │ :3000        │
                               │ (built by    │
                               │  AI agent)   │
                               └──────────────┘
                                       │
                                       ▼
                              ✅ Portfolio / todo app / etc loads

  If no app is running on port 3000, Caddy returns 502 Bad Gateway.
```

### 3c. Backend RPC (Tardi Backend → OpenClaw)

```
Tardi Backend (Cloud Run)
    │
    │  ws://<IPv4>:18789/?token=<OPENCLAW_AUTH_TOKEN>
    │  (Plain WS — no TLS needed for server-to-server)
    │  (Bypasses Cloudflare AND Caddy — uses IP:port directly)
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
NOT through Cloudflare or Caddy. UFW allows port 18789
from backend egress CIDRs.
```

### 3d. Heartbeat (VPS → Backend)

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
│    target_ver    │
│    preview_domain│  ← used by heartbeat to configure Caddy
│  }               │
└──────────────────┘
```

### 3e. Internal Tool Calls (Agent → Gateway)

```
OpenClaw Agent (inside openclaw-gateway container)
    │
    │  ws://127.0.0.1:18789
    │  (host network — loopback goes directly to gateway)
    │  (bypasses Caddy — Caddy only handles external :80)
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
│     (single container — Caddy is unaffected)                │
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

Key: Caddy is not restarted during config sync. Only the
OpenClaw container is recreated. Caddy continues routing.
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
         │    Response: { config_version, target_openclaw_version,
         │               preview_domain }
         │
         ├──▶ Sync PREVIEW_DOMAIN from API → .env (if missing)
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
              ├── Auth: auth.mode=token, trustedProxies, allowInsecureAuth
              ├── Caddy: binary installed, Caddyfile correct, service running
              ├── iptables: REMOVE old NAT rule if still present
              ├── UFW hardening: restrict 18789 to CF CIDRs + backend CIDRs
              ├── Cloudflare IPs: refresh daily from cloudflare.com
              ├── Backend egress CIDRs: apply to 18789 + 22 if set
              └── Control UI: dangerouslyDisableDeviceAuth=true
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
│   Both DNS records point to the same VPS IP.           │
│   Caddy routes by hostname on the VPS.                 │
├────────────────────────────────────────────────────────┤
│ Step 3: WaitServerReady (5 min)                        │
│   Poll Hetzner API until status = "running"            │
├────────────────────────────────────────────────────────┤
│ Step 4: Bootstrap (10 min)                             │
│   Wait for cloud-init to complete                      │
│   Verify SSH connectivity                              │
│   cloud-init installs Docker, configures firewall,     │
│   creates users, writes configs, pulls images,         │
│   downloads Caddy binary, writes Caddyfile,            │
│   starts Caddy systemd service                         │
├────────────────────────────────────────────────────────┤
│ Step 5: InstallAgent (10 min)                          │
│   Wait for openclaw-gateway container to be healthy    │
│   Apply post-startup config (model, telegram)          │
├────────────────────────────────────────────────────────┤
│ Step 6: Activate (1 min)                               │
│   Mark instance as "active" in DB                      │
│   Start heartbeat timer                                │
└────────────────────────────────────────────────────────┘
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
│                  │   root + key auth  │ Script push      │
│                  │                    │ Status check     │
│                  │                    │                  │
│                  │── WS :18789 ──────▶│ RPC calls        │
│                  │   ?token=xxx       │ (config.patch,   │
│                  │   direct to IP     │  channels.status)│
│                  │   (bypasses Caddy) │                  │
│                  │                    │                  │
│                  │── Hetzner API ─────│ Create/Delete    │
│                  │   (not to VPS)     │ Snapshot/Rebuild │
└──────────────────┘                    └──────────────────┘

Backend RPC uses plain ws:// on port 18789 directly to IP.
Does not go through Cloudflare or Caddy.
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
│ Both records point to the SAME VPS IP.                      │
│ Caddy on the VPS routes by Host header:                     │
│   <uuid>.tardi.ai   → OpenClaw (:18789)                    │
│   <uuid>-b.tardi.ai → User app (:3000)                     │
│                                                             │
│ Domain scheme: FLAT (single level subdomain)                │
│   <uuid>.tardi.ai     (covered by Universal SSL *.tardi.ai) │
│   <uuid>-b.tardi.ai   (also covered by *.tardi.ai)          │
└─────────────────────────────────────────────────────────────┘
```

## 10. Caddyfile (Hostname-Based Routing)

```
# /etc/caddy/Caddyfile
# Installed by cloud-init, maintained by heartbeat drift guard.
# No TLS — Cloudflare handles TLS at the edge (Flexible SSL mode).

http://<uuid>-b.tardi.ai {
    reverse_proxy localhost:3000
}

http:// {
    reverse_proxy localhost:18789
}
```

How it works:
- Caddy listens on port 80 as a systemd service (runs as root)
- The specific `http://<uuid>-b.tardi.ai` block matches the preview domain
- The `http://` catch-all matches everything else (dashboard domain, direct IP, etc.)
- Traffic to localhost:3000/18789 bypasses UFW (loopback is always allowed)
- If no app is running on port 3000, Caddy returns 502 Bad Gateway

## 11. docker-compose.yml (Single Container)

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

# No 'caddy' service (Caddy runs as a host systemd service, not Docker)
# No 'networks' section
# No port mappings (host network exposes directly)
```

## 12. openclaw.json

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
    "auth": {
      "mode": "token"
    }
  }
}
```

- `trustedProxies: ["0.0.0.0/0"]` — Caddy (and Cloudflare) add proxy headers;
  without this OpenClaw treats connections as untrusted and won't grant operator scopes.
- `allowInsecureAuth: true` — required for shared token auth to grant operator scopes.

## 13. .env File

```bash
DOCKER_GID=<gid>
AGENT_TOKEN=<64-char-hex>
API_URL=https://api-dev.tardi.ai  # or api.tardi.ai for prod
INSTANCE_ID=<uuid>
OPENCLAW_AUTH_TOKEN=<64-char-hex>
OPENCLAW_GATEWAY_TOKEN=<64-char-hex>  # same value as OPENCLAW_AUTH_TOKEN
OPENROUTER_API_KEY=<key>
NODE_ENV=production
PREVIEW_DOMAIN=<uuid>-b.tardi.ai      # used by heartbeat to write Caddyfile
BACKEND_EGRESS_CIDRS=<cidr1>,<cidr2>   # optional, restricts UFW SSH + 18789

# Optional (set by user in dashboard):
ANTHROPIC_API_KEY=<key>
OPENAI_API_KEY=<key>
TELEGRAM_BOT_TOKEN=<token>
```

## 14. Architecture Evolution

| Aspect | Phase 1 (2 containers) | Phase 2 (iptables NAT) | Phase 3 (Caddy routing) |
|--------|----------------------|----------------------|------------------------|
| Port 80 routing | Caddy Docker container | iptables NAT → 18789 | **Caddy host binary** |
| Preview domain | Not functional | Routed to OpenClaw (broken) | **Routes to port 3000** |
| Hostname routing | None | None | **By Host header** |
| Docker networking | Bridge (openclaw-net) | Host (--network=host) | Host (--network=host) |
| TLS termination | Caddy (Let's Encrypt) | Cloudflare Proxy (edge) | Cloudflare Proxy (edge) |
| Caddy runs as | Docker container | Not present | **Systemd service (host)** |
| Backend RPC | `wss://` through Caddy | `ws://:18789` direct | `ws://:18789` direct |
| Cert management | Caddy auto-renewal | None (Cloudflare) | None (Cloudflare) |
| DDoS protection | None | Cloudflare (free tier) | Cloudflare (free tier) |

## Key Source Files

- `backend/internal/jobs/provisioner.go` — Cloud-init template: installs Caddy, writes Caddyfile, starts systemd service
- `backend/internal/scripts/heartbeat.go` — Caddy drift guard: ensures binary, Caddyfile, service are correct; removes old iptables NAT
- `backend/internal/api/agent.go` — Heartbeat API: returns `preview_domain` so existing VPSes can configure Caddy
- `backend/internal/api/sync.go` — Config sync script (Caddy unaffected by container restarts)
- `backend/internal/dns/cloudflare.go` — Creates both DNS records with `proxied: true`
