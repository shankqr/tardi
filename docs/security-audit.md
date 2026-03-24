# Security Audit — Tardi VPS Single-Container Architecture

> **Date:** 2026-03-24
> **Scope:** Full security analysis of the single-container VPS architecture with host networking and Cloudflare Proxy TLS termination.
> **Codebase state:** Post-UFW hardening (commit `a76aba6` — port 18789 restricted to Cloudflare IPs, port 22 restricted to backend egress CIDRs).

---

## Architecture Overview

```
Internet → Cloudflare (TLS) → VPS:80 (HTTP) → iptables NAT → 0.0.0.0:18789 (OpenClaw)
                                VPS:22 (SSH) ← Backend (Cloud Run) for config sync
                                VPS:18789 ← Backend (WebSocket RPC, direct IP, restricted by UFW)
```

**Container:** `network_mode: host`, UID 1000, Docker socket mounted, no resource limits.
**Firewall:** UFW default deny. Port 80 open. Port 18789 restricted to Cloudflare + backend egress CIDRs. Port 22 restricted to backend egress CIDRs (falls back to open when not configured).

---

## 1. Attack Surface

| Entry Point | Auth | Encryption | Exposed To | Status |
|---|---|---|---|---|
| VPS :80 (HTTP via Cloudflare) | OpenClaw token (hash fragment) | Cloudflare edge TLS | Internet via Cloudflare | **OK** — protected by Cloudflare proxy |
| VPS :18789 (WebSocket direct) | OpenClaw token in URL query | **None (plaintext ws://)** | Cloudflare IPs + backend egress CIDRs | **HARDENED** — no longer open to all |
| VPS :22 (SSH) | Root password (24-char hex) | SSH encryption | Backend egress CIDRs (or all if unconfigured) | **PARTIAL** — needs `BACKEND_EGRESS_CIDRS` |
| Backend API (Cloud Run) | Firebase JWT | Cloud Run TLS | Internet | **OK** |
| Backend → VPS RPC | Token in `ws://` URL | **None** | Cloud Run → internet → VPS | **RISK** — plaintext token on wire |
| Agent → Backend heartbeat | Agent token Bearer | HTTPS | VPS → Cloud Run | **OK** |
| Agent → Gateway (loopback) | OPENCLAW_GATEWAY_TOKEN env var | None (localhost) | 127.0.0.1 only | **OK** |

---

## 2. Firewall & Network Security

### 2a. Current UFW Rules (post-hardening)

| Port | Rule | Source Restriction | Purpose |
|---|---|---|---|
| 80/tcp | ALLOW | Anywhere | Receives Cloudflare Proxy traffic (HTTP Flexible SSL) |
| 18789 | ALLOW | Cloudflare IPv4 CIDRs | NAT'd port-80 traffic (PREROUTING rewrites dest to 18789 before UFW INPUT) |
| 18789 | ALLOW | `BACKEND_EGRESS_CIDRS` | Backend WebSocket RPC (direct to VPS IP) |
| 22/tcp | ALLOW | `BACKEND_EGRESS_CIDRS` | SSH config sync (when CIDRs configured) |
| 22/tcp | ALLOW | Anywhere | SSH fallback (when `BACKEND_EGRESS_CIDRS` not set) |
| All others | DENY | — | Default deny incoming |

**Files:**
- Cloud-init: `backend/internal/jobs/provisioner.go` lines 96-117
- Heartbeat drift guard: `backend/internal/scripts/heartbeat.go` lines 161-211

### 2b. iptables NAT

```
iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789
```

- Applied in cloud-init and enforced every 5 minutes by heartbeat
- Persisted via `iptables-persistent`
- PREROUTING runs before UFW INPUT chain — UFW must allow 18789 for NAT'd traffic to pass

### 2c. Host Networking Implications

- Container shares host network namespace — can bind any port, see all interfaces
- Container runs as UID 1000 (cannot bind ports < 1024 without NAT)
- No network isolation between container and host processes
- Loopback traffic (127.0.0.1:18789) is indistinguishable from external traffic at the application level

### 2d. Cloudflare IP Refresh

- Fetched from `https://www.cloudflare.com/ips-v4` during cloud-init
- Refreshed daily by heartbeat via marker file `/opt/openclaw/.cf_ufw_updated`
- If Cloudflare adds new IP ranges, UFW rules update within 24 hours
- If fetch fails (network issue), existing rules remain — no data loss

### 2e. Remaining Network Risk: Port 80 Open to All

Port 80 accepts traffic from **any source**, not just Cloudflare. An attacker connecting directly to `:80` gets NAT'd to `:18789` (OpenClaw), bypassing Cloudflare. This is a lower risk than the old blanket `:18789` rule because:
- Cloudflare's `proxied: true` DNS hides the VPS IP from DNS lookups
- Attacker would need to discover the IP through other means (scan, leak)
- OpenClaw still requires token auth on WebSocket connect

**Recommendation:** Restrict port 80 to Cloudflare IPs as well (same approach as 18789). This would fully close the direct-IP bypass path. Not yet implemented because port 80 is the lower-risk entry (token still required) and was not in the original scope.

---

## 3. Authentication

### 3a. Token Types & Storage

| Token | Generation | Storage | Rotation | Expiry |
|---|---|---|---|---|
| AGENT_TOKEN (64-char hex) | `crypto/rand` at provisioning | DB: `agent_token_secret_name` (plaintext) | None | Never |
| OPENCLAW_AUTH_TOKEN (64-char hex) | `crypto/rand` at provisioning | DB: `openclaw_auth_token` (plaintext) | None | Never |
| OPENCLAW_GATEWAY_TOKEN | Same value as OPENCLAW_AUTH_TOKEN | VPS: `/opt/openclaw/.env` (chmod 600) | None | Never |
| Root Password (24-char hex) | `crypto/rand` at provisioning | DB: `root_password` (plaintext) | None | Never |
| Firebase JWT | Firebase Auth SDK | Client-side (browser) | Automatic | ~1 hour |
| Google OAuth tokens | OAuth2 flow | DB: AES-256-GCM encrypted | Auto-refresh by token_refresh job | Access: 1 hour, Refresh: long-lived |
| Admin API token | Manual configuration | Environment variable | Manual | Never |

**Files:**
- Token generation: `backend/internal/jobs/provisioner.go` (`GenerateAgentToken()`)
- Firebase auth: `backend/internal/api/middleware/auth.go`
- Google OAuth encryption: `backend/internal/crypto/tokens.go`
- Admin auth: `backend/internal/api/router.go` line 111

### 3b. Frontend → Backend (Firebase JWT)

- Production: `auth.Client.VerifyIDToken()` validates JWT signature and expiry
- Dev mode with `mock-token`: bypasses all auth (returns mock user)
- Dev mode without Firebase: treats bearer token string as UID (no validation)
- **Risk:** Dev mode must never be active in production. Gated on `cfg.IsDev()`.

**File:** `backend/internal/api/middleware/auth.go` lines 18-68

### 3c. VPS → Backend (Agent Token)

- Bearer token in Authorization header over HTTPS
- Looked up via `db.GetInstanceByAgentToken()` — maps token to instance
- No Firebase involved — separate auth path
- Used by: heartbeat, config fetch

**File:** `backend/internal/api/agent.go` lines 21-65

### 3d. Backend → VPS (SSH + WebSocket RPC)

**SSH:**
- Root user, password auth, `InsecureIgnoreHostKey()`
- No host key pinning (MITM-able on network path)
- Used for: config sync, script push, dashboard token generation, status checks

**WebSocket RPC:**
- Plain `ws://` (no TLS) to VPS IP on port 18789
- Token in URL query parameter: `ws://<ip>:18789/?token=<token>`
- Token visible to any network observer on the path
- Used for: `config.patch`, `config.get`, `channels.status`, `web.login.start`

**Files:**
- SSH: `backend/internal/sshexec/exec.go`
- WebSocket: `backend/internal/api/whatsapp.go` lines 186-316

### 3e. Browser → OpenClaw Control UI

- Token delivered via URL hash fragment: `https://<domain>/#token=<TOKEN>`
- Hash fragment is client-side only (not sent to server, not logged)
- Control UI JS reads token and includes in WebSocket `connect` message
- Device pairing disabled: `dangerouslyDisableDeviceAuth: true`
- `allowInsecureAuth: true` grants operator scopes via token auth

**Risk:** If token URL leaks (browser history, screen share, referrer), anyone can access the dashboard. Mitigated by 64-char hex entropy and hash-fragment delivery.

### 3f. Admin API

- `X-Admin-Token` header, simple string comparison
- No rate limiting on admin endpoints
- No audit logging of admin actions
- Empty token disables admin API entirely

**File:** `backend/internal/api/router.go` line 111

---

## 4. Secrets Management

### 4a. VPS Secrets (on-disk)

| File | Permissions | Contents |
|---|---|---|
| `/opt/openclaw/.env` | 600 (owner-only) | AGENT_TOKEN, OPENCLAW_AUTH_TOKEN, OPENCLAW_GATEWAY_TOKEN, API keys, TELEGRAM_BOT_TOKEN, BACKEND_EGRESS_CIDRS |
| `/opt/openclaw/data/openclaw/openclaw.json` | 1000:1000 | Gateway auth token (written by OpenClaw), auth mode, Control UI settings |
| `/opt/openclaw/data/openclaw/.config/gogcli/` | 600 | Google OAuth credentials.json and refresh tokens (base64-decoded) |

### 4b. Database Secrets (plaintext)

These are stored **unencrypted** in PostgreSQL:
- `root_password` — SSH root password for every VPS
- `openclaw_auth_token` — OpenClaw gateway auth token
- `agent_token_secret_name` — VPS heartbeat auth token (field name is misleading — stores plaintext, not a secret manager reference)

**Risk:** A database breach exposes root SSH access to **all active VPS instances**.
**Recommendation:** Encrypt with GCP KMS envelope encryption. Decrypt only when needed.

### 4c. Google OAuth Token Encryption

- Encrypted at rest with AES-256-GCM
- Key: 32-byte hex from `TOKEN_ENCRYPTION_KEY` env var
- Random nonce prepended to ciphertext
- Decrypted only during token refresh and config sync

**Files:** `backend/internal/crypto/tokens.go`, `backend/internal/jobs/token_refresh.go`

### 4d. Cloud-Init Secret Exposure

- Root password, agent token, and API keys embedded in cloud-init script
- Stored in Hetzner instance metadata (accessible via provider API)
- Logged to `/var/log/cloud-init-output.log` (world-readable by default)

**Recommendation:** Restrict log permissions. Clear instance metadata after first boot.

---

## 5. Container Security

### 5a. Docker Socket Mount (CRITICAL)

`/var/run/docker.sock` is mounted into the OpenClaw container. Combined with host networking, this grants **root-equivalent access** to the host:
- Spawn privileged containers
- Mount host filesystem (`-v /:/host`)
- Read all secrets from `/opt/openclaw/.env`
- Modify iptables rules, install packages

**Why it exists:** OpenClaw spawns sandboxed tool-execution containers via Docker API.

**Recommendation:** Use Docker socket proxy (e.g., `tecnativa/docker-socket-proxy`) to restrict API calls to `containers` and `images` endpoints only.

**File:** `backend/internal/jobs/provisioner.go` line 228

### 5b. No Resource Limits

Docker Compose has no `mem_limit`, `cpus`, `pids_limit`, or `ulimits`. A runaway AI agent or malicious tool can consume all host resources.

**Recommendation:** Add to docker-compose template:
```yaml
mem_limit: 3g
cpus: "3"
pids_limit: 512
```

### 5c. Container Runs as UID 1000

- Non-root user (good)
- Docker group membership grants Docker API access (negates non-root benefit for container escape)
- Cannot bind ports < 1024 (requires iptables NAT for port 80)

---

## 6. Backend API Security

### 6a. Rate Limiting (BROKEN)

Rate limiter uses `r.RemoteAddr` which, behind Cloud Run's load balancer, returns the **proxy IP** — not the client IP. All requests from a single ingress point share the same bucket.

- Global: 60 req/min per IP — effectively per-proxy, not per-client
- Provisioning: 10 req/min for POST/DELETE on `/api/instances`
- In-memory storage — lost on restart, grows unbounded (memory leak)

**Files:** `backend/internal/api/middleware/ratelimit.go`

**Recommendation:**
1. Use `X-Forwarded-For` header (Cloud Run sets this reliably) for client IP
2. Add periodic cleanup goroutine for stale limiter entries
3. Consider per-user rate limiting (via Firebase UID) instead of per-IP

### 6b. CORS Configuration

- Origins from `ALLOWED_ORIGINS` env var (comma-separated)
- Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
- Headers: Authorization, Content-Type
- Credentials: allowed
- Preflight cache: 300 seconds

**File:** `backend/internal/api/middleware/cors.go`

### 6c. SQL Injection

All database queries use parameterized queries with `$1, $2` placeholders. No raw SQL concatenation with user input found.

**File:** `backend/internal/db/queries.go`

### 6d. Missing Security Headers

No `Strict-Transport-Security`, `X-Content-Type-Options`, `X-Frame-Options`, or `Content-Security-Policy` headers. Partially mitigated by Cloud Run (forces HTTPS) and Cloudflare (adds some headers at edge).

### 6e. OAuth State Store (Multi-Instance CSRF Risk)

OAuth CSRF state tokens are stored **in-memory** with `sync.Mutex`. Cloud Run can scale to multiple instances — state generated on instance A won't be found on instance B.

Code acknowledges this: *"consider using the DB or a signed JWT"*.

**Impact:** OAuth flow may fail intermittently. If state validation is bypassed, CSRF attacks on OAuth become possible.

**Recommendation:** Use HMAC-signed state tokens (stateless validation) or move to database.

**File:** `backend/internal/api/google_oauth.go` lines 36-52

### 6f. OAuth postMessage Wildcard Origin

```javascript
window.opener.postMessage(msg, '*');
```

OAuth callback sends result to **any opener window**. A malicious page that opens the OAuth popup could intercept the result.

**Recommendation:** Replace `'*'` with the specific frontend origin from config.

**File:** `backend/internal/api/google_oauth.go` line 395

---

## 7. SSH Security

### 7a. Root Password Auth Enabled

- `PermitRootLogin yes` + `PasswordAuthentication yes` in sshd_config
- Cloud-init sets password via `chpasswd`
- Heartbeat re-enables password auth every 5 minutes (overrides cloud-init's default disable)
- No `fail2ban` or SSH rate limiting

**Files:**
- Cloud-init: `backend/internal/jobs/provisioner.go` lines 120-124
- Heartbeat: `backend/internal/scripts/heartbeat.go` lines 14-20

### 7b. InsecureIgnoreHostKey

`ssh.InsecureIgnoreHostKey()` — no host key verification. MITM attack on the network path between Cloud Run and VPS can intercept root password.

**Recommendation:** Record host key at provisioning time (first SSH connection or Hetzner API) and verify on subsequent connections.

**File:** `backend/internal/sshexec/exec.go` line 20

### 7c. SSH Access Control (current state)

- **With `BACKEND_EGRESS_CIDRS` set:** UFW restricts port 22 to backend egress IPs only
- **Without `BACKEND_EGRESS_CIDRS`:** Port 22 open to all sources (fallback)
- No SSH key auth — password only
- Password is 24-char hex (~96 bits entropy) — strong against online brute force

**Action needed:** Configure Cloud NAT with static egress IP and set `BACKEND_EGRESS_CIDRS` env var on Cloud Run to lock down SSH.

---

## 8. Heartbeat & Drift Guards

The heartbeat script runs every 5 minutes via systemd timer and enforces:

| Guard | What it checks | What it fixes |
|---|---|---|
| SSH password auth | `/etc/ssh/sshd_config.d/60-tardi.conf` exists | Rewrites and restarts sshd |
| iptables NAT | `PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789` | Adds rule and saves |
| UFW hardening | Blanket `18789/tcp ALLOW Anywhere` exists | Replaces with per-Cloudflare-CIDR rules |
| Cloudflare IPs | Marker file older than 24 hours | Fetches and adds new CIDRs |
| Backend egress CIDRs | `BACKEND_EGRESS_CIDRS` in `.env` | Applies to UFW for 18789 + 22 |
| Gateway auth mode | `auth.mode != "token"` in openclaw.json | Rewrites to `token` mode |
| `allowInsecureAuth` | `controlUi.allowInsecureAuth != true` | Sets to `true` |
| `dangerouslyDisableDeviceAuth` | Not set in openclaw.json | Sets to `true`, restarts container |
| OPENCLAW_GATEWAY_TOKEN sync | `.env` token != `openclaw.json` token | Updates `.env` |
| Telegram config | `streaming != "off"` or `enabled != true` | Applies correct settings via CLI |
| Model drift | No model set after restart | Re-applies from backend API |
| Orphaned Caddy | `openclaw-caddy` container exists | Removes container + image |
| 2-container migration | `docker-compose.yml` has caddy/bridge | Rewrites to single container + host networking |

**File:** `backend/internal/scripts/heartbeat.go`

---

## 9. Token Lifecycle & Rotation

| Token | Created | Rotated | Revoked | Expires |
|---|---|---|---|---|
| AGENT_TOKEN | Provisioning | Never | Instance deletion | Never |
| OPENCLAW_AUTH_TOKEN | Provisioning | Never | Instance deletion | Never |
| Root Password | Provisioning | Never | Instance deletion | Never |
| Dashboard Device Token | On-demand (`/dashboard-token`) | Each request generates new | Previous stays valid | Never |
| Firebase JWT | User login | Automatic (Firebase SDK) | User logout / Firebase admin | ~1 hour |
| Google OAuth Access | OAuth flow | `token_refresh` job | User disconnects Google | 1 hour |
| Google OAuth Refresh | OAuth flow | Never (until revoked) | User disconnects Google | Long-lived |

**Gap:** No rotation for VPS tokens (AGENT_TOKEN, OPENCLAW_AUTH_TOKEN, root password). A leaked token remains valid for the lifetime of the VPS.

**Recommendation:** Implement periodic rotation (e.g., on each config sync or monthly). Add token versioning in DB to support revocation.

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

**Recommendation:** Add manual approval step for production Terraform applies. Enable dependency vulnerability scanning.

---

## 11. Findings Summary

### Mitigated (by recent hardening)

| # | Finding | Status |
|---|---|---|
| ~~1~~ | ~~Port 18789 open to all sources~~ | **FIXED** — Restricted to Cloudflare + backend egress CIDRs |
| ~~2~~ | ~~SSH open to all sources~~ | **PARTIAL** — Restricted when `BACKEND_EGRESS_CIDRS` set, open otherwise |

### CRITICAL (requires immediate action)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 1 | Docker socket mount = root-equivalent container escape | `provisioner.go:228` | Docker socket proxy to restrict API calls |
| 2 | No token rotation — leaked tokens valid forever | `provisioner.go` | Periodic rotation + DB versioning |

### HIGH (address within 1-2 weeks)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 3 | Backend→VPS RPC over plaintext `ws://` (token on wire) | `whatsapp.go:191` | Route through Cloudflare domain (`wss://`) or WireGuard tunnel |
| 4 | SSH `InsecureIgnoreHostKey()` enables MITM | `exec.go:20` | Pin host key at provisioning time |
| 5 | Root passwords stored plaintext in DB | DB schema | KMS envelope encryption |
| 6 | OAuth state in-memory (multi-instance CSRF) | `google_oauth.go:36-52` | HMAC-signed state tokens or DB |
| 7 | Rate limiter uses `RemoteAddr` (broken behind proxy) | `ratelimit.go:18` | Use `X-Forwarded-For` |
| 8 | No container resource limits | `provisioner.go` compose template | Add `mem_limit`, `cpus`, `pids_limit` |
| 9 | Complete SSH lockdown pending | UFW rules | Configure Cloud NAT + set `BACKEND_EGRESS_CIDRS` |
| 10 | Port 80 still open to all sources (direct IP bypass possible) | `provisioner.go:99` | Restrict to Cloudflare IPs (same as 18789) |

### MEDIUM (address within 1 month)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 11 | OAuth `postMessage(msg, '*')` | `google_oauth.go:395` | Use specific frontend origin |
| 12 | Cloud-init secrets in metadata/logs | `provisioner.go` | Restrict log perms, clear metadata |
| 13 | `dangerouslyDisableDeviceAuth: true` | `provisioner.go:158` | Acceptable trade-off; add session expiry |
| 14 | Admin API no rate limit/audit logging | `router.go:111` | Rate limit + audit log |
| 15 | No centralized audit logging | — | Log auth failures, SSH, RPC, admin actions |
| 16 | Rate limiter memory leak | `ratelimit.go` | Periodic cleanup goroutine |

### LOW (backlog)

| # | Finding | File | Recommendation |
|---|---|---|---|
| 17 | `allowedOrigins: ["*"]` in OpenClaw | `provisioner.go:157` | Mitigated by token requirement |
| 18 | Heartbeat JSON via `printf` (injection risk) | `heartbeat.go` | Use `jq` for safe JSON construction |
| 19 | No HTTP security headers | — | Add HSTS, X-Content-Type-Options, CSP |
| 20 | Terraform auto-apply without review | `deploy-infra.yml` | Add approval gate |
| 21 | No dependency vulnerability scanning | CI/CD | Enable Dependabot or Snyk |

---

## 12. Recommended Remediation Order

### Phase 1 — Immediate (before next user onboarding)
1. Configure Cloud NAT with static egress IP → set `BACKEND_EGRESS_CIDRS` → complete SSH lockdown
2. Restrict port 80 to Cloudflare IPs (same approach as 18789)
3. Fix rate limiter to use `X-Forwarded-For`

### Phase 2 — Short-term (1-2 weeks)
4. Add container resource limits (`mem_limit: 3g`, `cpus: 3`, `pids_limit: 512`)
5. Encrypt root passwords and tokens in DB with KMS
6. Fix OAuth state store (HMAC-signed tokens)
7. Pin SSH host keys at provisioning
8. Fix `postMessage('*')` in OAuth callback
9. Add rate limiter cleanup goroutine
10. Add `fail2ban` for SSH in cloud-init

### Phase 3 — Medium-term (1 month)
11. Docker socket proxy to restrict container API access
12. Backend→VPS RPC over `wss://` through Cloudflare or WireGuard
13. Token rotation mechanism
14. Centralized audit logging
15. Security headers middleware
16. Dependency vulnerability scanning

---

## 13. Verification Checklist

- [ ] **Port scan** a VPS from external IP — only 80 should respond from arbitrary IPs
- [ ] **Direct WebSocket** to `ws://<vps-ip>:18789/health` from non-Cloudflare IP — should be blocked by UFW
- [ ] **Direct WebSocket** from Cloudflare IP range — should succeed (for NAT'd traffic)
- [ ] **SSH** from non-backend IP — should be blocked when `BACKEND_EGRESS_CIDRS` is set
- [ ] **Rate limiter test** — verify different clients get independent rate limit buckets
- [ ] **Cloud-init logs** — check `/var/log/cloud-init-output.log` permissions
- [ ] **Cloudflare IP refresh** — verify `find /opt/openclaw/.cf_ufw_updated -mmin +1440` triggers refresh
- [ ] **Token in DB** — verify `root_password` and `openclaw_auth_token` columns contain plaintext (confirms encryption needed)
