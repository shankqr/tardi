# Security Review — Tardi OpenClaw-on-VPS Stack

> **Date:** 2026-04-17
> **Scope:** Fresh re-audit of the OpenClaw-on-VPS attack surface at current `dev` branch state.
> **Supersedes:** [security-audit.md](security-audit.md) (2026-03-26). Old doc kept for history.
> **Use this doc as:** the triage source for security tickets, and the roadmap gate for the follow-up hardening work (Docker sandbox, mitmproxy egress firewall, Clawvisor host watchdog).

---

## 1. Threat model

- **Tenant is untrusted.** Each VPS runs an OpenClaw agent whose behavior is driven by third-party prompts and tool output. Assume an adversary can, at any moment, steer the agent toward arbitrary code execution and arbitrary network requests inside its container.
- **Backend is trusted.** Cloud Run; Firebase-authenticated user + admin API; operator-controlled deploys.
- **VPS host is semi-trusted.** Once a tenant escapes the container, host and tenant share fate. The blast radius is one user's instance + its secrets — but it can become the control plane's problem via credential reuse or supply-chain loops.

### What changed since the 2026-03-26 audit

| Change | Impact |
|--------|--------|
| Migration 034 ([`034_add_custom_caddyfile.sql`](../backend/migrations/034_add_custom_caddyfile.sql)) added `vps_instances.custom_caddyfile`. The value is piped through the heartbeat response at [`api/agent.go:208-216`](../backend/internal/api/agent.go#L208-L216) and consumed by the VPS-side bash at [`scripts/heartbeat.go:90`](../backend/internal/scripts/heartbeat.go#L90). | New tenant → control-plane write path that reaches a root-run reverse proxy. |
| Migration 036 ([`036_add_framework.sql`](../backend/migrations/036_add_framework.sql)) added `framework` (openclaw/hermes) with a second heartbeat variant ([`heartbeat_hermes.go`](../backend/internal/scripts/heartbeat_hermes.go)) and provisioner ([`provisioner_hermes.go`](../backend/internal/jobs/provisioner_hermes.go)). | Doubles the attack surface and the drift-correction logic. |
| Migration 038 ([`038_pin_hermes_v090.sql`](../backend/migrations/038_pin_hermes_v090.sql)) pinned the hermes version. | Good — partial fix for supply-chain risk. The openclaw image tag is still unpinned. |

---

## 2. Findings

Severity is CVSSv4-style (0–10). Every row cites the primary evidence. "Status" is relative to this 2026-04-17 review.

### 2.1 Boundary (internet → VPS)

| ID  | Finding | Evidence | CVSS | Status |
|-----|---------|----------|:----:|--------|
| B-1 | Caddy runs as root on :80 | [`provisioner.go:252-265`](../backend/internal/jobs/provisioner.go#L252-L265) | 6.1 | Open — tradeoff accepted (needs port 80) |
| B-2 | Backend → VPS control plane uses `ws://` with token in URL query | [`api/sync.go:28`](../backend/internal/api/sync.go#L28) | 7.4 | Open |
| B-3 | `trustedProxies: ["0.0.0.0/0"]` in OpenClaw config | [`provisioner.go:181`](../backend/internal/jobs/provisioner.go#L181) | 5.3 | Open, partially mitigated (Caddy strips upstream XFF) |
| B-4 | `custom_caddyfile` flows DB → heartbeat JSON → root-owned reverse proxy with no schema validation | [`migrations/034`](../backend/migrations/034_add_custom_caddyfile.sql), [`scripts/heartbeat.go:90`](../backend/internal/scripts/heartbeat.go#L90), [`api/agent.go:216`](../backend/internal/api/agent.go#L216) | 6.8 | **New since 2026-03-26** |

**B-1** — Caddy listens on 80 because Cloudflare is in "Flexible" TLS mode. Deprivileging Caddy via `CAP_NET_BIND_SERVICE` is the right long-term fix; not blocking for this review.

**B-2** — Every `config.patch`/`config.get` sends the OpenClaw token in the URL. Anyone with passive network visibility between Cloud Run egress and the VPS (or holding Cloudflare logs for direct-IP hits) captures the token. Fix: tunnel the RPC through Clawvisor's mTLS channel, or upgrade to `wss://` with token in header.

**B-4** — Tenant can store a Caddyfile fragment in their instance row. The heartbeat pushes it verbatim; the drift-correction on the VPS writes it into `/etc/caddy/Caddyfile` and reloads Caddy. No allow-list of directives, no schema. A malicious tenant can point traffic at attacker hosts or disable security headers. Fix: schema-validate on write in the backend and integrity-check on the VPS via Clawvisor.

### 2.2 Tenant (agent → host)

| ID  | Finding | Evidence | CVSS | Status |
|-----|---------|----------|:----:|--------|
| T-1 | Docker socket mounted into the OpenClaw container | [`provisioner.go:286`](../backend/internal/jobs/provisioner.go#L286) | 9.8 | Open |
| T-2 | No `pids_limit`, `mem_limit`, `cpus`, `no-new-privileges`, `read_only`, seccomp, or AppArmor on the container | [`provisioner.go:273-296`](../backend/internal/jobs/provisioner.go#L273-L296) | 8.6 | Open |
| T-3 | Sandbox sibling containers (OpenClaw tool exec) inherit zero restrictions because they're spawned via raw `docker run` against the mounted socket | [`provisioner.go:155-159`](../backend/internal/jobs/provisioner.go#L155-L159) + runtime behavior | 8.2 | Open |
| T-4 | Unrestricted outbound network from the container | No egress filter anywhere in the repo | 7.5 | Open |
| T-5 | `network_mode: host` removes the L3 boundary between container and host | [`provisioner.go:279`](../backend/internal/jobs/provisioner.go#L279) | 6.5 | Open, deferred |

**T-1** is the single highest-impact finding. A tenant with RCE inside OpenClaw writes `docker run --privileged -v /:/host ...` and walks out as root on the host. The `user: "1000:1000"` constraint on the OpenClaw process is irrelevant because the Docker daemon runs as root and the socket grants full API access.

**T-2 / T-3** amplify T-1: once outside, there is no cgroup limit, no capability dropping, no filesystem protection, and no spawned-container restriction. A runaway or hostile agent can fork-bomb, eat all RAM, or launch arbitrary privileged siblings.

**T-4** means any credential the agent can read (the API keys in `.env`, anything in `/home/node/.config/gogcli/`) can be exfiltrated in plain HTTP to any host on the internet. Also enables C2-style persistence and SSRF to internal Hetzner metadata.

**T-5** is documented and deferred: bridging would break the current Caddy → `localhost:18789` path without extra plumbing (host-gateway extra_hosts). Pick up with sandbox hardening v2.

### 2.3 Control plane (backend → VPS)

| ID  | Finding | Evidence | CVSS | Status |
|-----|---------|----------|:----:|--------|
| C-1 | SSH from backend uses `ssh.InsecureIgnoreHostKey()` | [`sshexec/shell.go:110`](../backend/internal/sshexec/shell.go#L110) | 7.4 | Open |
| C-2 | Tokens and root password stored plaintext in `vps_instances` | [`migrations/010`](../backend/migrations/010_add_root_password.sql), [`migrations/015`](../backend/migrations/015_add_openclaw_auth_token.sql) | 8.1 | Open |
| C-3 | No token rotation path anywhere | Repo-wide absence | 7.2 | Open |
| C-4 | `audit_log` is used for user-facing CRUD but not for security-sensitive control-plane actions | Writers at [`api/instances.go:129,187,244,361`](../backend/internal/api/instances.go#L129), [`api/snapshots.go:94,167,222`](../backend/internal/api/snapshots.go#L94), [`api/webhooks.go:130`](../backend/internal/api/webhooks.go#L130); **no writer** in [`api/sync.go`](../backend/internal/api/sync.go), [`api/admin.go`](../backend/internal/api/admin.go), [`sshexec/`](../backend/internal/sshexec/) | 6.5 | Partial — forensics gap |
| C-5 | `/api/admin/*` endpoints have no per-action audit log | [`api/admin.go`](../backend/internal/api/admin.go) | 6.0 | Open |

**C-1** — Host keys aren't pinned, so any attacker with transient network control between Cloud Run and the VPS can MITM SSH. Combined with B-2 (plaintext RPC), the backend→VPS path has no strong authenticator. Fix: capture the host key fingerprint at provisioning time, verify on every subsequent dial.

**C-2/C-3** — Tokens are 64 hex chars with good entropy, but plaintext at rest in Postgres and the VPS `.env`, and they never rotate. A read of the `vps_instances` table = every live dashboard hijacked until instance termination. Fix: KMS envelope encryption + scheduled rotation via the heartbeat/Clawvisor channel.

**C-4** — The existing audit writers are for user-visible lifecycle events (create/delete instance, snapshot ops, webhook triggers). Missing: `config.patch` RPC calls, SSH sessions, admin version updates, admin password resets, dashboard-token reads. Adding these gives us forensics without new infrastructure.

**C-5** — Admin endpoints (global/per-instance version pin, root password reset-by-IP) are protected by `X-Admin-Token` middleware but not logged per-action. Under a compromise of the admin token, we'd have no timeline.

### 2.4 Supply chain

| ID  | Finding | Evidence | CVSS | Status |
|-----|---------|----------|:----:|--------|
| S-1 | `ghcr.io/openclaw/openclaw:{tag}` is pulled by tag, never by digest | [`provisioner.go:154,276`](../backend/internal/jobs/provisioner.go#L154) | 7.8 | Open |
| S-2 | Caddy binary downloaded from `caddyserver.com/api/download` with no version pin and no SHA verification | [`provisioner.go:233-237`](../backend/internal/jobs/provisioner.go#L233-L237) | 6.5 | Open |
| S-3 | `ghcr.io/openclaw/openclaw-sandbox:bookworm-slim` is unpinned | [`provisioner.go:157`](../backend/internal/jobs/provisioner.go#L157) | 7.1 | Open |
| S-4 | Agent-initiated tool installs (npm/pip/apt/curl) have no allow-list or audit | Runtime behavior | 5.5 | Accepted for now; bounded by egress allow-list once T-4 is fixed |

**S-1/S-2/S-3** — A tag hijack or compromised upstream (GHCR namespace takeover, tag force-push, caddyserver.com MITM) rolls a malicious binary into every VPS on the next heartbeat-driven image pull. Fix: resolve tag → `sha256:...` server-side once per release and pin the digest; SHA-verify the Caddy binary against a published checksum.

**S-4** — The agent can `pip install attacker_pkg` and run it. We won't fully close this without a sandbox that blocks network, which is impractical. The right mitigation is Feature C's egress allow-list (block unknown package hosts) plus logging of every HTTP URL the agent fetches.

### 2.5 Data at rest

| ID  | Finding | Evidence | CVSS | Status |
|-----|---------|----------|:----:|--------|
| D-1 | All user API keys (OpenRouter, Anthropic, OpenAI, Google OAuth client/token) stored plaintext in `agent_configs.config` JSONB | [`migrations/006`](../backend/migrations/006_create_agent_configs.sql) | 8.1 | Open |
| D-2 | `/opt/openclaw/.env` is mode 600, but readable by any root-equivalent process (i.e. anyone who exploits T-1/T-2) | [`provisioner.go:219`](../backend/internal/jobs/provisioner.go#L219) | 6.5 | Open, mitigated once T-1/T-2 are fixed |
| D-3 | No disk encryption on Hetzner volume | Platform default | 4.5 | Accepted — Hetzner-layer concern |

---

## 3. Delta vs 2026-03-26 audit

- **Fixed since 2026-03-26:** port 18789 restricted to Cloudflare + backend CIDRs ([`provisioner.go:95-117`](../backend/internal/jobs/provisioner.go#L95-L117)); SSH password auth disabled ([`provisioner.go:125-137`](../backend/internal/jobs/provisioner.go#L125-L137)); iptables NAT replaced by Caddy reverse proxy; rate limiter now uses `X-Forwarded-For`.
- **Still open:** every row in §2 without a "Fixed" status — matches the old audit's outstanding items.
- **New:** B-4 (`custom_caddyfile` write path); hermes framework duplication (doubles the surface but inherits the same structural issues).

---

## 4. Remediation roadmap

Every finding is mapped to one of three follow-up features. All three are scoped in [the companion plan](../../.claude/plans/redo-the-security-review-swift-bird.md).

| Finding(s) | Fixed by | Notes |
|------------|----------|-------|
| T-1 | Docker sandbox — swap raw socket for `docker-socket-proxy`, allow-list API calls | Blocks `--privileged`, bind mounts, and arbitrary volumes |
| T-2 | Docker sandbox — `cap_drop: [ALL]`, `read_only: true`, `no-new-privileges`, seccomp+AppArmor profiles, `pids_limit: 512`, `mem_limit: 2560m`, `cpus: 1.8` | Sized for cx22 |
| T-3 | Docker sandbox — socket-proxy flags force sandbox siblings onto a pre-created bridge with no volumes | Restricts what OpenClaw can spawn |
| T-4 | Mitmproxy egress firewall — explicit HTTP(S)_PROXY + iptables fail-closed + SNI bypass for LLM providers | Allow-list in DB, pushed by heartbeat |
| S-1, S-3 | Digest resolver (central backend job) — tag → `sha256:...` written to `vps_instances.openclaw_image_digest`; provisioner + heartbeat render `image: ghcr.io/…@sha256:…` | One job per release, not per VPS |
| S-2 | Pin Caddy version + SHA256 verify in provisioner | Small change, same phase as sandbox |
| B-2 | Deferred short-term; Clawvisor's own channel uses mTLS. Closing B-2 on OpenClaw's `:18789` needs upstream support | Revisit after Clawvisor ships |
| B-4 | Caddyfile schema validator in backend + Clawvisor file-integrity check on `/etc/caddy/Caddyfile` | Two-sided defense |
| C-1 | Pin SSH host key at provisioning, verify on every dial | In-place fix to [`sshexec/shell.go:110`](../backend/internal/sshexec/shell.go#L110) |
| C-4, C-5 | Add `audit_log` writes in `sync.go`, `admin.go`, `sshexec/`, dashboard-token handler; Clawvisor + mitmproxy become writers too | The table already exists and has writers; we're filling gaps |
| Liveness / integrity / killswitch / resource watchdog | Clawvisor | New Go binary, systemd unit on VPS, mTLS to backend |

---

## 5. Deferred items

Tracked here so they don't get forgotten; scheduled after the three primary features land.

- **KMS envelope encryption** for `openclaw_auth_token`, `root_password`, `agent_configs.config` (closes **C-2**, **D-1**). Use GCP KMS; reuse the existing `backend/internal/crypto` package as the envelope.
- **Caddy deprivilege** via `CAP_NET_BIND_SERVICE` or `authbind` (closes **B-1**).
- **OpenClaw RPC tunnel through Clawvisor** (closes **B-2** for the `:18789` channel) — needs upstream OpenClaw support for non-URL auth, or a local Clawvisor-terminated mTLS tunnel.
- **Hermes framework coverage** for sandbox + mitmproxy + Clawvisor. Scoped out of v1 to keep the blast radius small during rollout.
- **Token rotation** (closes **C-3**) — design once Clawvisor's channel is load-bearing.
- **Network bridge for the OpenClaw container** (closes **T-5**) — requires reworking the Caddy → `localhost:18789` hop.

---

## 6. Cross-check

Every finding ID in §2 appears in the §4 roadmap or the §5 deferred list. No orphan findings.

| Category | Count |
|----------|:----:|
| Boundary | 4 |
| Tenant | 5 |
| Control plane | 5 |
| Supply chain | 4 |
| Data at rest | 3 |
| **Total** | **21** |
