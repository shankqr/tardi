# VPS Architecture — Diagrams, Flows & Connections

## 1. VPS Container Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        VPS (Ubuntu 24.04)                           │
│                        Hetzner Cloud                                │
│                                                                     │
│  ┌──────────────── UFW Firewall ──────────────────────────────┐     │
│  │  ALLOW: 22/tcp (SSH), 80/tcp (HTTP), 443/tcp (HTTPS)       │     │
│  │  DENY:  everything else                                    │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌─────────────── Docker Network: openclaw-net (bridge) ────── ┐    │
│  │                                                             │    │
│  │  ┌─────────────────────┐     ┌────────────────────────── ┐  │    │
│  │  │   openclaw-caddy    │     │   openclaw-gateway        │  │    │
│  │  │   (caddy:2-alpine)  │     │   (ghcr.io/openclaw/      │  │    │
│  │  │                     │     │    openclaw:<tag>)        │  │    │
│  │  │  Ports:             │     │                           │  │    │
│  │  │  0.0.0.0:80  → :80  │     │  Port:                    │  │    │
│  │  │  0.0.0.0:443 → :443 │     │  127.0.0.1:18789 → :18789 │  │    │
│  │  │                     │     │  (loopback only!)         │  │    │
│  │  │  Volumes:           │     │                           │  │    │
│  │  │  ./Caddyfile (ro)   │────▶│  Volumes:                 │  │    │
│  │  │  ./certs (ro)       │http │  ./data/openclaw →        │  │    │
│  │  │  ./caddy/data       │     │    /home/node/.openclaw   │  │    │
│  │  │  ./caddy/config     │     │  /var/run/docker.sock     │  │    │
│  │  │                     │     │    (tool execution)       │  │    │
│  │  │  depends_on:        │     │                           │  │    │
│  │  │    gateway(healthy) │     │  User: 1000:1000          │  │    │
│  │  └─────────────────────┘     │  Healthcheck:             │  │    │
│  │                              │    curl localhost:18789   │  │    │
│  │         ▲                    │    /health (30s interval) │  │    │
│  │         │                    └────────────────────────── ┘  │    │
│  └─────────┼───────────────────────────────────────────────────┘    │
│            │                                                        │
│  ┌─────────┴────────────────────────────────────────────────── ┐    │
│  │  Systemd Services                                           │    │
│  │  ├── openclaw-stack.service (docker compose up -d)          │    │
│  │  ├── openclaw-heartbeat.timer (every 5 min)                 │    │
│  │  └── openclaw-heartbeat.service (runs heartbeat.sh)         │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  /opt/openclaw/                                                     │
│  ├── .env                    # All tokens & API keys                │
│  ├── .config_version         # Last synced config version           │
│  ├── docker-compose.yml      # Service definitions                  │
│  ├── Caddyfile               # Reverse proxy config                 │
│  ├── heartbeat.sh            # Heartbeat script (from backend)      │
│  ├── certs/                  # Self-signed TLS (if no domain)       │
│  ├── caddy/{data,config}     # Let's Encrypt cert persistence       │
│  └── data/openclaw/          # OpenClaw runtime data                │
│      └── openclaw.json       # Runtime config (OpenClaw-owned)      │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. Network Flow — External Traffic

```
                    Internet
                       │
                       ▼
          ┌────────────────────────┐
          │  UFW Firewall          │
          │  allow 80, 443, 22     │
          └────────┬───────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
   Port 80/443           Port 22
        │                     │
        ▼                     ▼
  ┌───────────┐         ┌─────────┐
  │   Caddy   │         │  sshd   │
  │           │         │         │
  │ :80 → 301 │         │ root pw │
  │   HTTPS   │         │  auth   │
  │           │         └─────────┘
  │ TLS term  │
  │ (LE or    │
  │ self-sign)│
  └─────┬─────┘
        │
        │  http://openclaw-gateway:18789
        │  (Docker DNS resolution)
        ▼
  ┌────────────────┐
  │ OpenClaw       │
  │ Gateway        │
  │                │
  │ :18789         │
  │ WebSocket +    │
  │ HTTP + Control │
  │ UI (Lit SPA)   │
  └────────────────┘
```

## 3. Authentication Flows

### 3a. Dashboard Access (Browser → OpenClaw Control UI)

```
Browser (User clicks "Open Agent Dashboard")
    │
    │  https://<domain>/#token=<OPENCLAW_AUTH_TOKEN>
    │
    ▼
┌─────────┐                    ┌──────────────┐
│  Caddy  │ ── reverse_proxy ──▶ OpenClaw     │
│ (TLS)   │                    │ Gateway      │
└─────────┘                    └──────┬───────┘
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
                              { auth: { token: "xxx" } }
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
    │  wss://<IPv4>/?token=<OPENCLAW_AUTH_TOKEN>
    │  (InsecureSkipVerify for self-signed certs)
    │
    ▼
┌─────────┐                    ┌──────────────┐
│  Caddy  │ ── reverse_proxy ──▶ OpenClaw     │
│ (TLS)   │                    │ Gateway      │
└─────────┘                    └──────┬───────┘
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
```

### 3d. Internal Tool Calls (Agent → Gateway)

```
OpenClaw Agent (inside openclaw-gateway container)
    │
    │  ws://127.0.0.1:18789
    │  (bypasses Caddy entirely!)
    │
    ▼
OpenClaw Gateway (same container)
    │
    │  Auth: OPENCLAW_GATEWAY_TOKEN env var
    │  (this is why auth.mode must be "token",
    │   NOT "trusted-proxy" — no proxy headers
    │   on internal loopback calls)
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
              ├── Auth: auth.mode=token with correct scopes
              ├── DNS: disable stub listener if active
              ├── Caddyfile: clean transparent proxy
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
│   DNS: create A record (abc12345.agents.tardi.ai)      │
├────────────────────────────────────────────────────────┤
│ Step 3: WaitServerReady (5 min)                        │
│   Poll Hetzner API until status = "running"            │
├────────────────────────────────────────────────────────┤
│ Step 4: Bootstrap (10 min)                             │
│   Wait for cloud-init to complete                      │
│   Verify SSH connectivity                              │
│   cloud-init installs Docker, configures firewall,     │
│   creates users, writes configs, pulls images          │
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
│                  │   root + password  │ Script push      │
│                  │                    │ Status check     │
│                  │                    │                  │
│                  │── WSS :443 ───────▶│ RPC calls        │
│                  │   ?token=xxx       │ (config.patch,   │
│                  │   via Caddy        │  channels.status)│
│                  │                    │                  │
│                  │── Hetzner API ─────│ Create/Delete    │
│                  │   (not to VPS)     │ Snapshot/Rebuild │
└──────────────────┘                    └──────────────────┘
```

## Key Source Files
- `backend/internal/jobs/provisioner.go` — Cloud-init template, provisioning pipeline
- `backend/internal/scripts/heartbeat.go` — Heartbeat script constant
- `backend/internal/api/sync.go` — Config sync SSH script
- `backend/internal/api/telegram.go` — Telegram RPC patching
- `backend/internal/api/whatsapp.go` — OpenClaw WebSocket RPC protocol
- `backend/internal/provider/hetzner/hetzner.go` — Hetzner provider implementation
- `backend/internal/sshexec/exec.go` — SSH execution wrapper
