# Security Audit — Tardi VPS Platform

> **Date:** 2026-03-26 (full re-audit)
> **Scope:** Full security analysis of the single-container VPS architecture with Caddy host binary, Cloudflare Proxy TLS, and token-based gateway auth.
> **Previous audit:** 2026-03-24 (single-container migration, SSH hardening)

---

## Architecture Overview

```
Internet → Cloudflare (TLS termination) → VPS:80 (Caddy, HTTP) → localhost:18789 (OpenClaw)
                                           VPS:22 (SSH) ← Backend (Cloud Run) for config sync
                                           VPS:18789 ← Backend (WebSocket RPC, restricted by UFW)
```

**Container:** Single `openclaw-gateway`, `network_mode: host`, UID 1000, Docker socket mounted, no resource limits.
**Reverse proxy:** Caddy runs as host binary (systemd service, port 80, runs as root). Hostname-based routing: preview domain → :3000, all else → :18789.
**Firewall:** UFW default deny. Port 80 open to all. Port 18789 restricted to Cloudflare + backend egress CIDRs. Port 22 restricted to backend egress CIDRs (falls back to open if not configured).
**TLS:** Cloudflare edge only. No self-signed certs. Caddy receives plaintext HTTP from Cloudflare.

---

## 1. Attack Surface

| Entry Point | Auth | Encryption | Exposed To | Status |
|---|---|---|---|---|
| VPS :80 (Caddy → OpenClaw) | OpenClaw token (hash fragment → WebSocket) | Cloudflare edge TLS | Internet via Cloudflare | **OK** |
| VPS :18789 (WebSocket direct) | OpenClaw token in URL query | **None (plaintext ws://)** | Cloudflare IPs + backend egress CIDRs | **HARDENED** |
| VPS :22 (SSH) | Ed25519 key only | SSH encryption | Backend egress CIDRs (or all if unconfigured) | **HARDENED** |
| Backend API (Cloud Run) | Firebase JWT | Cloud Run TLS | Internet | **OK** |
| Backend → VPS RPC | Token in `ws://` URL | **None** | Cloud Run → internet → VPS | **RISK** — plaintext token on wire |
| Agent → Backend heartbeat | Agent token Bearer | HTTPS | VPS → Cloud Run | **OK** |
| Agent → Gateway (loopback) | OPENCLAW_GATEWAY_TOKEN env var | None (localhost) | 127.0.0.1 only | **OK** |

---

## 2. Firewall & Network Security

### 2a. Current UFW Rules

| Port | Rule | Source Restriction | Purpose |
|---|---|---|---|
| 80/tcp | ALLOW | Anywhere | Caddy HTTP reverse proxy (Cloudflare origin traffic) |
| 18789 | ALLOW | Cloudflare IPv4 CIDRs | Backend WebSocket RPC (direct to VPS IP) |
| 18789 | ALLOW | `BACKEND_EGRESS_CIDRS` | Backend WebSocket RPC (direct to VPS IP) |
| 22/tcp | ALLOW | `BACKEND_EGRESS_CIDRS` | SSH config sync (when CIDRs configured). Key-only auth. |
| 22/tcp | ALLOW | Anywhere | SSH fallback (when `BACKEND_EGRESS_CIDRS` not set). Key-only. |
| All others | DENY | — | Default deny incoming |

**Files:**
- Cloud-init: `backend/internal/jobs/provisioner.go` lines 95-117
- Heartbeat drift guard: `backend/internal/scripts/heartbeat.go` lines 328-377

### 2b. iptables NAT — Decommissioned

The old `iptables -t nat PREROUTING :80 → :18789` redirect has been **removed** and replaced with Caddy reverse proxy. The heartbeat actively removes any lingering NAT rules (`heartbeat.go` lines 252-255).

### 2c. Caddy as Host Binary

Caddy runs as a systemd service (`caddy.service`) with **root** privileges on port 80. It performs hostname-based routing:
- Preview domain (e.g., `abc12345-b.tardi.ai`) → `localhost:3000` (user-built apps)
- All other traffic → `localhost:18789` (OpenClaw gateway)

**Custom Caddyfile support:** Per-instance custom Caddyfiles can be set via the `custom_caddyfile` DB field. The heartbeat syncs it from the API and replaces the local file if changed.

**Risk:** Caddy runs as root. A Caddy vulnerability could lead to full host compromise. Acceptable trade-off for binding port 80 without iptables NAT.

### 2d. Host Networking

- Container shares host network namespace — can bind any port, see all interfaces
- Container runs as UID 1000 (cannot bind ports < 1024 without NAT)
- No network isolation between container and host processes
- OpenClaw listens on `0.0.0.0:18789` — exposed on all interfaces, restricted by UFW

### 2e. Cloudflare IP Refresh

- Fetched from `https://www.cloudflare.com/ips-v4` during cloud-init and heartbeat
- Refreshed daily via marker file `/opt/openclaw/.cf_ufw_updated` (1440-minute check)
- If fetch fails, existing rules remain

### 2f. Remaining Network Risk: Port 80 Open to All

Port 80 accepts traffic from **any source**, not just Cloudflare. An attacker connecting directly to `:80` reaches Caddy, which proxies to OpenClaw. OpenClaw still requires token auth, and Cloudflare's `proxied: true` DNS hides the VPS IP from DNS lookups.

**Recommendation:** Restrict port 80 to Cloudflare IPs (same approach as 18789) for full defense-in-depth.

---

## 3. Authentication

### 3a. Token Types & Storage

| Token | Generation | Storage | Rotation | Expiry |
|---|---|---|---|---|
| AGENT_TOKEN (64-char hex) | `crypto/rand` at provisioning | DB: plaintext | None | Never |
| OPENCLAW_AUTH_TOKEN (64-char hex) | `crypto/rand` at provisioning | DB: plaintext | None | Never |
| OPENCLAW_GATEWAY_TOKEN | Same value as OPENCLAW_AUTH_TOKEN | VPS: `/opt/openclaw/.env` (chmod 600) | None | Never |
| Root Password (24-char hex) | `crypto/rand` at provisioning | DB: plaintext | None | Never — **unused** (password auth disabled) |
| SSH Private Key (Ed25519) | One-time manual generation | GCP Secret Manager (base64 PEM) | Manual | Never |
| Firebase JWT | Firebase Auth SDK | Client-side (browser) | Automatic | ~1 hour |
| Google OAuth tokens | OAuth2 flow | DB: AES-256-GCM encrypted | Auto-refresh by token_refresh job | Access: 1 hour, Refresh: long-lived |
| Admin API token | Manual configuration | Environment variable | Manual | Never |

**Files:**
- Token generation: `backend/internal/jobs/provisioner.go` lines 1004-1019
- Firebase auth: `backend/internal/api/middleware/auth.go`
- Google OAuth encryption: `backend/internal/crypto/tokens.go`
- Admin auth: `backend/internal/api/router.go` lines 104-120

### 3b. Frontend → Backend (Firebase JWT)

- Production: `auth.Client.VerifyIDToken()` validates JWT signature and expiry
- Dev mode with `mock-token`: bypasses all auth (returns mock user `mock-uid-12345`)
- Dev mode without Firebase: treats bearer token string as UID (no validation)
- **Risk:** Dev mode must never be active in production. Gated on `cfg.IsDev()`.

**File:** `backend/internal/api/middleware/auth.go` lines 18-68

### 3c. VPS → Backend (Agent Token)

- Bearer token in Authorization header over HTTPS
- Looked up via `db.GetInstanceByAgentToken()` — maps token to instance
- No Firebase involved — separate auth path

**File:** `backend/internal/api/agent.go`

### 3d. Backend → VPS (SSH + WebSocket RPC)

**SSH:**
- Root user, Ed25519 key-based auth (password auth disabled on VPS)
- Private key stored in GCP Secret Manager, mounted as `SSH_PRIVATE_KEY` env var on Cloud Run
- Public key injected into `/root/.ssh/authorized_keys` via cloud-init
- `PermitRootLogin prohibit-password` + `PasswordAuthentication no` enforced by heartbeat drift guard
- `InsecureIgnoreHostKey()` — no host key pinning (MITM-able on network path)

**WebSocket RPC:**
- Plain `ws://` (no TLS) to VPS IP on port 18789
- Token in URL query parameter: `ws://<ip>:18789/?token=<token>`
- Token visible to any network observer on the path
- Used for: `config.patch`, `config.get`, `channels.status`, `web.login.start`

**Files:**
- SSH: `backend/internal/sshexec/exec.go` line 40
- WebSocket: `backend/internal/api/whatsapp.go` line 191

### 3e. Browser → OpenClaw Control UI

- Token delivered via URL hash fragment: `https://<domain>/#token=<TOKEN>`
- Hash fragment is client-side only (not sent to server, not logged)
- Control UI JS reads token and includes in WebSocket `connect` message
- Device pairing disabled: `dangerouslyDisableDeviceAuth: true`
- `allowInsecureAuth: true` grants operator scopes via shared token auth (required since OC 2026.3.22+)

**Risk:** If token URL leaks (browser history, screen share, referrer), anyone can access the dashboard. Mitigated by 64-char hex entropy and hash-fragment delivery.

### 3f. Admin API

- `X-Admin-Token` header, simple string comparison (`router.go` lines 104-120)
- Empty token disables admin API entirely (returns 403)
- Admin endpoints: version management, password reset by IP
- No dedicated rate limiting (inherits global 60 req/min)
- No audit logging of admin actions

**File:** `backend/internal/api/router.go` lines 76-120

---

## 4. Secrets Management

### 4a. VPS Secrets (on-disk)

| File | Permissions | Contents |
|---|---|---|
| `/opt/openclaw/.env` | 600 (owner-only) | AGENT_TOKEN, OPENCLAW_AUTH_TOKEN, OPENCLAW_GATEWAY_TOKEN, API keys, TELEGRAM_BOT_TOKEN, BACKEND_EGRESS_CIDRS |
| `/root/.ssh/authorized_keys` | 600 (root-only) | Backend Ed25519 public key (same key across all VPSes) |
| `/opt/openclaw/data/openclaw/openclaw.json` | 1000:1000 | Gateway config, auth mode (token value read from env, not stored here) |
| `/opt/openclaw/data/gogcli/` | 600 | Google OAuth credentials.json and refresh tokens |

### 4b. Database Secrets (plaintext)

Stored **unencrypted** in PostgreSQL:
- `root_password` — **no longer grants SSH** (password auth disabled). Kept for Hetzner API compatibility.
- `openclaw_auth_token` — grants OpenClaw dashboard access
- `agent_token_secret_name` — VPS heartbeat auth token (name is misleading — stores plaintext token, not a secret manager reference)

**Risk:** Database breach exposes dashboard access tokens.
**Recommendation:** Encrypt with GCP KMS envelope encryption.

### 4c. Google OAuth Token Encryption

- Encrypted at rest with AES-256-GCM (random nonce prepended)
- Key: 32-byte hex from `TOKEN_ENCRYPTION_KEY` env var
- Decrypted only during token refresh and config sync

**Files:** `backend/internal/crypto/tokens.go`, `backend/internal/jobs/token_refresh.go`

### 4d. Cloud-Init Secret Exposure

- Root password, agent token, API keys, and provider tokens embedded in cloud-init script
- Stored in Hetzner instance metadata (accessible via provider API)
- Logged to `/var/log/cloud-init-output.log` (world-readable by default)

**Recommendation:** Restrict log permissions. Clear instance metadata after first boot.

---

## 5. Container Security

### 5a. Docker Socket Mount (CRITICAL)

`/var/run/docker.sock` is mounted into the OpenClaw container. Combined with host networking and Docker group membership, this grants **root-equivalent access** to the host:
- Spawn privileged containers
- Mount host filesystem (`-v /:/host`)
- Read all secrets from `/opt/openclaw/.env`
- Modify iptables rules, install packages

**Why it exists:** OpenClaw spawns sandboxed tool-execution containers via Docker API.

**Recommendation:** Docker socket proxy (e.g., `tecnativa/docker-socket-proxy`) to restrict API calls to `containers` and `images` endpoints only.

**File:** `backend/internal/jobs/provisioner.go` line 296

### 5b. No Resource Limits

Docker Compose has no `mem_limit`, `cpus`, `pids_limit`, or `ulimits`. A runaway agent or malicious tool can consume all host resources.

**Recommendation:** Add to docker-compose template:
```yaml
mem_limit: 3g
cpus: "3"
pids_limit: 512
```

### 5c. Container Runs as UID 1000

- Non-root user (good)
- Docker group membership grants Docker API access (negates non-root benefit for container escape)
- Cannot bind ports < 1024 (Caddy host binary handles port 80)

### 5d. `trustedProxies: ["0.0.0.0/0"]`

OpenClaw is configured to trust proxy headers from **any source IP**. This is safe because auth is enforced via token mode (not trusted-proxy mode), but it means OpenClaw will trust any `X-Forwarded-For` header for logging/display purposes. Not exploitable for auth bypass in token mode.

---

## 6. Backend API Security

### 6a. Rate Limiting — FIXED

Rate limiter now correctly uses `X-Forwarded-For` header for client IP extraction (`ratelimit.go` lines 14-27). Falls back to `RemoteAddr` only if header is absent.

- General: 60 req/min per client IP
- Provisioning: 10 req/min for POST/DELETE on `/api/instances`
- Health endpoints exempt (`/healthz`, `/readyz`)

**Remaining issue:** In-memory storage — limiter map grows unbounded. No periodic cleanup goroutine.

**File:** `backend/internal/api/middleware/ratelimit.go`

### 6b. CORS Configuration

- Origins from `ALLOWED_ORIGINS` env var (comma-separated)
- Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
- Headers: Authorization, Content-Type
- Credentials: allowed
- Preflight cache: 300 seconds

**File:** `backend/internal/api/middleware/cors.go`

### 6c. SQL Injection — SAFE

All database queries use parameterized queries with `$1, $2` placeholders. No raw SQL concatenation found.

### 6d. Missing Security Headers

No `Strict-Transport-Security`, `X-Content-Type-Options`, `X-Frame-Options`, or `Content-Security-Policy` headers. Partially mitigated by Cloud Run (forces HTTPS) and Cloudflare (can add headers at edge).

### 6e. OAuth State Store (Multi-Instance CSRF Risk)

OAuth CSRF state tokens stored **in-memory** with `sync.Mutex` (`google_oauth.go` lines 36-80). Cleanup of expired entries happens on each `Set()` call. One-time use enforced.

**Risk:** Cloud Run can scale to multiple instances — state from instance A won't be found on instance B. Comment acknowledges this, assumes 10-min TTL means "almost certainly hit the same instance."

**Recommendation:** Use HMAC-signed state tokens (stateless validation) or move to database.

### 6f. OAuth postMessage Wildcard Origin

```javascript
window.opener.postMessage(msg, '*');
```

OAuth callback sends result to **any opener window** (`google_oauth.go` line 395). A malicious page that opens the OAuth popup could intercept the result.

**Recommendation:** Replace `'*'` with the specific frontend origin.

---

## 7. SSH Security

### 7a. Key-Based Auth Only

- `PermitRootLogin prohibit-password` + `PasswordAuthentication no` in sshd_config
- Ed25519 public key injected into `/root/.ssh/authorized_keys` during cloud-init
- Private key stored in GCP Secret Manager (`SSH_PRIVATE_KEY`), mounted on Cloud Run
- Single key pair for all VPSes (blast radius: backend compromise = all VPS access)
- Root password still set via `chpasswd` (Hetzner API compat) but sshd rejects password auth

### 7b. SSH Key Drift Guard

- Heartbeat checks if old `PasswordAuthentication yes` config exists
- Only flips to `no` if `/root/.ssh/authorized_keys` has content (safety check)

**File:** `backend/internal/scripts/heartbeat.go` lines 14-25

### 7c. InsecureIgnoreHostKey

`ssh.InsecureIgnoreHostKey()` — no host key verification (`sshexec/exec.go` line 40). MITM on the Cloud Run → VPS path could hijack the SSH session. Risk reduced with key-based auth (no password to steal), but session content is interceptable.

**Recommendation:** Record host key at provisioning, verify on subsequent connections.

---

## 8. Heartbeat & Drift Guards

The heartbeat script runs every 5 minutes via systemd timer and enforces:

| Guard | What it checks | What it fixes |
|---|---|---|
| SSH key-only auth | `PasswordAuthentication yes` in sshd config | Flips to `no` + `prohibit-password` if authorized_keys has content |
| iptables NAT removal | Old `PREROUTING :80 → :18789` rule | Removes rule (replaced by Caddy) |
| Caddy binary | `/usr/local/bin/caddy` missing | Downloads and installs Caddy |
| Caddy service | Not running or Caddyfile drifted | Creates systemd service, starts/reloads |
| Custom Caddyfile | API returns `custom_caddyfile` | Replaces local Caddyfile with custom version |
| UFW hardening | Blanket `18789/tcp ALLOW Anywhere` | Replaces with per-Cloudflare-CIDR rules |
| Cloudflare IPs | Marker file older than 24 hours | Fetches fresh Cloudflare IP list |
| Backend egress CIDRs | `BACKEND_EGRESS_CIDRS` in `.env` | Applies to UFW for 18789 + 22 |
| Gateway auth mode | `auth.mode != "token"` in openclaw.json | Rewrites to `token` mode, force-recreates if crash-looping |
| `allowInsecureAuth` | `controlUi.allowInsecureAuth != true` | Sets to `true` |
| `trustedProxies` | Missing in openclaw.json | Sets to `["0.0.0.0/0"]` |
| `dangerouslyDisableDeviceAuth` | Not set in openclaw.json | Sets to `true` |
| OPENCLAW_GATEWAY_TOKEN sync | `.env` token != running container token | Updates `.env` |
| Telegram config | `streaming != "off"` or `enabled != true` | Applies correct settings via CLI |
| Telegram account overrides | Per-account dmPolicy/streaming drifted | Fixes account-level settings |
| Model drift | No model set after restart | Re-applies from backend API |
| Orphaned Caddy container | `openclaw-caddy` Docker container exists | Removes container + image (migration cleanup) |

**File:** `backend/internal/scripts/heartbeat.go`

---

## 9. Token Lifecycle & Rotation

| Token | Created | Rotated | Revoked | Expires |
|---|---|---|---|---|
| AGENT_TOKEN | Provisioning | Never | Instance deletion | Never |
| OPENCLAW_AUTH_TOKEN | Provisioning | Never | Instance deletion | Never |
| Root Password | Provisioning | Never | Instance deletion | Never — **unused** |
| SSH Private Key | One-time manual | Manual | Secret Manager version delete | Never |
| Firebase JWT | User login | Automatic (Firebase SDK) | User logout / admin | ~1 hour |
| Google OAuth Access | OAuth flow | `token_refresh` job (5-min interval) | User disconnects Google | 1 hour |
| Google OAuth Refresh | OAuth flow | Never (until revoked) | User disconnects Google | Long-lived |

**Gap:** No rotation for VPS tokens. A leaked token remains valid for the lifetime of the VPS.

---

## 10. CI/CD Security

| Aspect | Status | Notes |
|---|---|---|
| GCP auth | Workload Identity Federation | No long-lived service account keys |
| Secrets injection | GitHub Actions secrets → Cloud Run env | Via Secret Manager at runtime |
| Branch protection | `dev` → dev env, `main` → prod env | Separate deployment targets |
| Terraform | `apply -auto-approve` on push to main | No manual review gate |
| Docker images | Multi-stage Alpine build, `CGO_ENABLED=0` | Minimal attack surface |
| Dependency scanning | Not configured | No Dependabot or Snyk |

---

## 11. Findings Summary

### Mitigated (by previous hardening)

| # | Finding | Status |
|---|---|---|
| ~~1~~ | ~~Port 18789 open to all sources~~ | **FIXED** — Restricted to Cloudflare + backend egress CIDRs |
| ~~2~~ | ~~SSH open to all with password auth~~ | **FIXED** — Key-only auth, password disabled |
| ~~3~~ | ~~Root passwords in DB grant SSH access~~ | **FIXED** — sshd rejects password auth |
| ~~4~~ | ~~SSH password/IP exposed in frontend UI~~ | **FIXED** — Removed from dashboard |
| ~~5~~ | ~~iptables NAT complexity~~ | **FIXED** — Replaced with Caddy reverse proxy |
| ~~6~~ | ~~Rate limiter uses RemoteAddr~~ | **FIXED** — Now uses X-Forwarded-For correctly |

### CRITICAL (requires immediate action)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 1 | Docker socket mount = root-equivalent container escape | `provisioner.go:296` | Docker socket proxy to restrict API calls |
| 2 | No token rotation — leaked tokens valid forever | `provisioner.go` | Periodic rotation + DB versioning |

### HIGH (address within 1-2 weeks)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 3 | Backend→VPS RPC over plaintext `ws://` (token on wire) | `whatsapp.go:191` | Route through Cloudflare domain (`wss://`) or WireGuard tunnel |
| 4 | SSH `InsecureIgnoreHostKey()` enables MITM | `exec.go:40` | Pin host key at provisioning |
| 5 | OpenClaw tokens stored plaintext in DB | DB schema | KMS envelope encryption |
| 6 | OAuth state in-memory (multi-instance CSRF) | `google_oauth.go:36-52` | HMAC-signed state tokens or DB |
| 7 | No container resource limits | `provisioner.go` compose template | Add `mem_limit`, `cpus`, `pids_limit` |
| 8 | Admin API: no audit logging, no dedicated rate limit | `admin.go`, `router.go` | Audit log for password resets + version changes; dedicated rate limit |
| 9 | Port 80 open to all sources (direct IP bypass) | `provisioner.go:98` | Restrict to Cloudflare IPs |

### MEDIUM (address within 1 month)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 10 | OAuth `postMessage(msg, '*')` | `google_oauth.go:395` | Use specific frontend origin |
| 11 | Cloud-init secrets in metadata/logs | `provisioner.go` | Restrict log perms, clear metadata |
| 12 | `dangerouslyDisableDeviceAuth: true` | `provisioner.go:185` | Acceptable trade-off; add session expiry |
| 13 | Rate limiter memory leak (no cleanup goroutine) | `ratelimit.go` | Periodic cleanup of stale entries |
| 14 | No centralized audit logging | — | Log auth failures, SSH, RPC, admin actions |
| 15 | Caddy runs as root | `heartbeat.go:307` | Acceptable for port 80 binding; no alternative without NAT |

### LOW (backlog)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 16 | `allowedOrigins: ["*"]` in OpenClaw | `provisioner.go:184` | Mitigated by token requirement |
| 17 | `trustedProxies: ["0.0.0.0/0"]` | `provisioner.go:182` | Safe in token mode; would matter in trusted-proxy mode |
| 18 | No HTTP security headers | — | Add HSTS, X-Content-Type-Options, CSP (partially covered by Cloudflare) |
| 19 | Terraform auto-apply without review | `deploy-infra.yml` | Add approval gate |
| 20 | No dependency vulnerability scanning | CI/CD | Enable Dependabot or Snyk |
| 21 | Single SSH key for all VPSes | `config.go` | Per-VPS keys or key rotation mechanism |

---

## 12. Recommended Remediation Order

### Phase 1 — Immediate (before next user onboarding)
1. Restrict port 80 to Cloudflare IPs (same approach as 18789)
2. Add container resource limits (`mem_limit: 3g`, `cpus: 3`, `pids_limit: 512`)

### Phase 2 — Short-term (1-2 weeks)
3. Encrypt OpenClaw tokens in DB with KMS
4. Fix OAuth state store (HMAC-signed tokens)
5. Pin SSH host keys at provisioning
6. Fix `postMessage('*')` in OAuth callback
7. Add audit logging for admin actions
8. Add rate limiter cleanup goroutine

### Phase 3 — Medium-term (1 month)
9. Docker socket proxy to restrict container API access
10. Backend→VPS RPC over `wss://` through Cloudflare or WireGuard
11. Token rotation mechanism
12. Centralized audit logging
13. Security headers middleware
14. Dependency vulnerability scanning

---

## 13. Changes Since Last Audit (2026-03-24)

| Change | Security Impact |
|---|---|
| iptables NAT removed, replaced with Caddy host binary | **Positive** — simpler, less fragile than kernel NAT rules |
| Heartbeat removes legacy NAT rules | **Positive** — migration cleanup |
| Custom Caddyfile support added | **Neutral** — new feature, no new attack surface (admin-controlled) |
| Rate limiter fixed to use X-Forwarded-For | **Positive** — per-client limiting now works correctly |
| `allowInsecureAuth: true` added | **Neutral** — required for OC 2026.3.22+ shared token auth to grant operator scopes |
| `trustedProxies: ["0.0.0.0/0"]` added | **Low risk** — safe in token mode (only affects proxy header trust, not auth) |
| Caddy drift guard in heartbeat | **Positive** — ensures Caddy binary + config stay correct |

---

## 14. Verification Checklist

- [x] **SSH password auth rejected** — `ssh -o PreferredAuthentications=password root@<ip>` returns `Permission denied`
- [x] **SSH key auth works** — `ssh -i ~/.ssh/tardi-backend root@<ip>` succeeds
- [x] **sshd config enforced** — `60-tardi.conf` contains `PasswordAuthentication no`, `PermitRootLogin prohibit-password`
- [x] **authorized_keys populated** — `/root/.ssh/authorized_keys` contains Ed25519 public key
- [x] **Rate limiter uses X-Forwarded-For** — `ratelimit.go` clientIP() checks header first
- [x] **iptables NAT removed** — heartbeat removes legacy rules
- [x] **Caddy running as systemd service** — `systemctl status caddy` active
- [ ] **Port scan** from external IP — only 80 should respond from arbitrary IPs
- [ ] **Direct WebSocket** to `ws://<vps-ip>:18789/health` from non-Cloudflare IP — should be blocked by UFW
- [ ] **SSH from non-backend IP** — should be blocked when `BACKEND_EGRESS_CIDRS` is set
- [ ] **Cloud-init logs** — check `/var/log/cloud-init-output.log` permissions
- [ ] **Cloudflare IP refresh** — verify daily refresh via marker file
- [ ] **Port 80 Cloudflare restriction** — not yet implemented (recommended)
