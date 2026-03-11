# Tardi

Dedicated AI agent hosting platform. Configure your agent, subscribe, and we handle the infrastructure.

## Stack

- **Frontend**: SvelteKit 2 + Svelte 5, Tailwind CSS 4, deployed on Cloudflare Pages
- **Backend**: Go on GCP Cloud Run, PostgreSQL on Cloud SQL
- **Auth**: Firebase Authentication
- **Payments**: Stripe
- **Infra**: Terraform (GCP for platform, Hetzner for VPS provisioning)
- **CI/CD**: GitHub Actions

## Project Structure

```
frontend/          SvelteKit app (dashboard, onboarding, landing page)
backend/           Go API server (control plane + orchestration)
infra/             Terraform configs (Cloud Run, Cloud SQL, Secrets, IAM)
.github/workflows/ CI/CD pipelines
docker-compose.yml Local Postgres for development
```

## Getting Started

### Frontend

```bash
cd frontend
npm install
npm run dev
```

The app runs at `http://localhost:5173`. Mock auth is enabled by default — sign in with any email/password.

### Backend

```bash
docker compose up -d   # Start local PostgreSQL
cd backend
make dev               # Hot reload on :8080
```

### Database (optional)

```bash
docker compose up -d
```

## Environment Variables

Copy `frontend/.env.example` and fill in Firebase credentials (not needed for mock mode):

```
VITE_FIREBASE_API_KEY=
VITE_FIREBASE_AUTH_DOMAIN=
VITE_FIREBASE_PROJECT_ID=
VITE_FIREBASE_STORAGE_BUCKET=
VITE_FIREBASE_MESSAGING_SENDER_ID=
VITE_FIREBASE_APP_ID=
```

Backend environment variables (see `backend/.env.example`):

```
PORT=8080
DATABASE_URL=postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable
ALLOWED_ORIGINS=http://localhost:5173
FIREBASE_PROJECT_ID=
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
HETZNER_API_TOKEN=
ENVIRONMENT=dev
```

## CI/CD

GitHub Actions automates all builds and deployments. Two branches serve as the source of truth:

- **`dev`** → development environment
- **`main`** → production environment

### Branching Flow

```
feature branch → PR to dev → merge → deploys to dev
                              ↓
               PR from dev to main → merge → deploys to prod
```

### Pipelines

#### On Pull Request (CI)

All PRs to `dev` or `main` trigger the **CI Gate** workflow, which detects which directories changed and runs only the relevant checks:

| Component | Checks |
|-----------|--------|
| Frontend (`frontend/**`) | TypeScript type check (`svelte-check`), production build |
| Backend (`backend/**`) | `golangci-lint`, `go test -race`, compile check |
| Infrastructure (`infra/**`) | `terraform fmt -check`, `terraform validate`, `terraform plan` (posted as PR comment) |

The CI Gate's `All Checks Passed` job is the single required status check for branch protection.

#### On Push to `dev` (Deploy to Dev)

| Component | Action |
|-----------|--------|
| Frontend | Build with Firebase env vars → `wrangler pages deploy --branch=dev` → [dev.tardi.pages.dev](https://dev.tardi.pages.dev) |
| Backend | Docker build → push to GCP Artifact Registry (`dev-{sha}` + `latest` tags) → deploy to Cloud Run `tardi-api-dev` |

#### On Push to `main` (Deploy to Prod)

| Component | Action |
|-----------|--------|
| Frontend | Build with Firebase env vars → `wrangler pages deploy --branch=main` → [tardi.pages.dev](https://tardi.pages.dev) |
| Backend | Docker build → push to GCP Artifact Registry (`prod-{sha}` + `stable` tags) → deploy to Cloud Run `tardi-api-prod` |
| Infrastructure | `terraform apply` with both dev and prod tfvars (unified state) |

### Workflow Files

```
.github/workflows/
  ci-gate.yml            # PR gate — detects changes, calls reusable workflows
  ci-frontend.yml        # Reusable: svelte-check + build
  ci-backend.yml         # Reusable: lint + test + build
  ci-infra.yml           # Reusable: terraform validate + plan
  deploy-frontend.yml    # Build + deploy to Cloudflare Pages
  deploy-backend.yml     # Docker build/push + Cloud Run deploy
  deploy-infra.yml       # Terraform apply (main branch only)
```

### Key Design Decisions

- **Path-based filtering**: Only changed components trigger CI/CD (e.g., frontend-only changes skip backend pipelines)
- **Infrastructure applies from `main` only**: Dev and prod share Terraform state (VPC, Artifact Registry). All infra changes require PR review
- **GCP Workload Identity Federation**: No long-lived service account keys — GitHub Actions authenticates via OIDC
- **Concurrency control**: Per-branch deployment groups prevent parallel deploys to the same environment
- **Docker image tags**: Immutable `{env}-{sha7}` for deploys, floating `latest`/`stable` for convenience

### Required GitHub Secrets

| Secret | Purpose |
|--------|---------|
| `GCP_PROJECT_ID` | GCP project identifier |
| `GCP_REGION` | GCP region (e.g., `us-central1`) |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation provider |
| `GCP_SERVICE_ACCOUNT` | CI/CD service account email |
| `CLOUDFLARE_API_TOKEN` | Cloudflare Pages deployment |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account |
| `VITE_FIREBASE_*` (6 secrets) | Firebase config for frontend builds |

### Branch Protection

- **`main`**: Requires PR review (1 approver), "CI Gate / All Checks Passed" status check, linear history, no direct push
- **`dev`**: Requires "CI Gate / All Checks Passed" status check

## VPS Provisioning & OpenClaw Dashboard Access

Each Tardi agent runs on a dedicated VPS with an OpenClaw gateway accessible via the browser. Getting this to work reliably required solving several interconnected problems across TLS, auth, reverse proxying, and WebSocket connections. This section documents the final working architecture and why each piece is necessary.

### Architecture Overview

```
Browser (user)
    │
    │  HTTPS (self-signed cert)
    ▼
┌──────────────────────────────────────────────┐
│  Caddy (reverse proxy)  :443                 │
│  - Validates ?token= or oc_sess cookie       │
│  - Sets X-Forwarded-User: owner header       │
│  - Proxies to OpenClaw via Docker network    │
└──────────────┬───────────────────────────────┘
               │  Docker bridge network
               ▼
┌──────────────────────────────────────────────┐
│  OpenClaw Gateway  :18789 (loopback only)    │
│  - auth.mode: trusted-proxy                  │
│  - Trusts X-Forwarded-User from proxy IPs    │
│  - Serves Control UI + WebSocket API         │
└──────────────────────────────────────────────┘
```

### The Problem Chain

OpenClaw's Control UI is a browser-based SPA that connects to its gateway via WebSocket. Making this accessible from a remote browser (not localhost) requires solving each of these problems in sequence:

1. **Secure context requirement**: The Control UI requires a secure context (HTTPS or localhost) for device identity features. Plain HTTP fails with "control ui requires device identity."
2. **Self-signed TLS**: VPS instances don't have domain names, only IPs. We generate a self-signed cert at boot with the server's IP as the SAN. Users accept the browser warning once.
3. **Origin/pairing checks**: OpenClaw enforces device pairing for non-local connections. Even spoofing `X-Forwarded-For: 127.0.0.1` and `Origin: localhost` doesn't fully bypass this.
4. **Trusted-proxy auth mode**: The breakthrough — OpenClaw's `trusted-proxy` auth mode delegates all authentication to the reverse proxy. The proxy sets `X-Forwarded-User` and OpenClaw trusts it, completely bypassing device pairing and token checks.
5. **WebSocket auth**: The browser's WebSocket API cannot set custom headers (like `Authorization`). Token-based auth breaks because the OpenClaw JS app creates WS connections without the `?token=` query param.
6. **Cookie-based session**: Solved by setting an `oc_sess` cookie on the initial `?token=` page load. The browser automatically sends cookies on WebSocket upgrade requests, so Caddy can authenticate WS connections.

### OpenClaw Gateway Config

The gateway config (`/opt/openclaw/data/openclaw/openclaw.json`) requires two related but distinct settings:

```json
{
  "gateway": {
    "bind": "lan",
    "controlUi": {
      "allowedOrigins": ["*"]
    },
    "trustedProxies": ["172.16.0.0/12", "10.0.0.0/8", "192.168.0.0/16"],
    "auth": {
      "mode": "trusted-proxy",
      "trustedProxy": {
        "userHeader": "X-Forwarded-User"
      }
    }
  }
}
```

| Key | Purpose |
|-----|---------|
| `gateway.bind: "lan"` | Binds to `0.0.0.0` so Caddy can reach it via Docker network. Without this, it only listens on loopback. |
| `gateway.controlUi.allowedOrigins: ["*"]` | Allows the Control UI to load from any origin (the browser sees the raw IP, not localhost). Without this: "origin not allowed." |
| `gateway.trustedProxies` | **IP allowlist** (top-level, plural). Tells OpenClaw which source IPs are allowed to send proxy headers. Docker bridge networks use `172.16.0.0/12`. Without this: "gateway auth mode=trusted-proxy requires gateway.trustedProxies." |
| `gateway.auth.mode: "trusted-proxy"` | Delegates authentication entirely to the reverse proxy. OpenClaw skips its own token/pairing checks. |
| `gateway.auth.trustedProxy.userHeader` | **Header name** (nested inside `auth`, singular). The HTTP header that identifies the authenticated user. Without this: "no trustedProxy config was provided (set gateway.auth.trustedProxy)." |

Note the confusing naming: `gateway.trustedProxies` (plural, top-level) is the IP allowlist, while `gateway.auth.trustedProxy` (singular, under `auth`) configures the header. Both are required.

### Caddy Reverse Proxy Config

The Caddyfile implements a cookie-based auth flow to handle both page loads and WebSocket connections:

```caddyfile
:443 {
    tls /etc/caddy/certs/cert.pem /etc/caddy/certs/key.pem

    # 1. Static assets (JS/CSS bundles) - public, no secrets
    @static path /assets/* /favicon.* /apple-touch-icon.png /__openclaw__/*
    handle @static {
        reverse_proxy openclaw-gateway:18789
    }

    # 2. Token auth (initial page load) - sets session cookie
    @auth_query query token={env.OPENCLAW_AUTH_TOKEN}
    handle @auth_query {
        header Set-Cookie "oc_sess={env.OPENCLAW_AUTH_TOKEN}; Path=/; Secure; HttpOnly; SameSite=None; Max-Age=86400"
        reverse_proxy openclaw-gateway:18789 {
            header_up X-Forwarded-User owner
            header_up Connection {header.Connection}
            header_up Upgrade {header.Upgrade}
        }
    }

    # 3. Cookie auth (WebSocket + subsequent requests)
    @auth_cookie expression {http.request.cookie.oc_sess} == {env.OPENCLAW_AUTH_TOKEN}
    handle @auth_cookie {
        reverse_proxy openclaw-gateway:18789 {
            header_up X-Forwarded-User owner
            header_up Connection {header.Connection}
            header_up Upgrade {header.Upgrade}
        }
    }

    # 4. Everything else gets 401
    respond 401
}
```

**Auth flow step by step:**

1. User clicks "Open" in the Tardi dashboard → browser navigates to `https://<IP>/?token=<token>`
2. Caddy matches `@auth_query`, sets `oc_sess` cookie with 24h expiry, proxies to OpenClaw with `X-Forwarded-User: owner`
3. OpenClaw serves the Control UI HTML, which loads JS/CSS from `/assets/*` (matched by `@static`, no auth needed)
4. The Control UI JS initiates a WebSocket connection to `wss://<IP>/` — the browser automatically sends the `oc_sess` cookie
5. Caddy matches `@auth_cookie`, proxies the WebSocket upgrade with `X-Forwarded-User: owner`
6. OpenClaw accepts the connection because the `X-Forwarded-User` header came from a trusted proxy IP

**Why each cookie attribute matters:**
- `Secure`: Required because we're on HTTPS (even self-signed)
- `HttpOnly`: Prevents JS from reading the token (XSS protection)
- `SameSite=None`: Required for the cookie to be sent on WebSocket upgrade requests from the same origin. `Strict` or `Lax` can block WS upgrades in some browsers
- `Max-Age=86400`: 24-hour session so users don't need to re-authenticate constantly

### Security Model

- **Gateway isolation**: OpenClaw binds to `127.0.0.1:18789` on the host (Docker port mapping). Direct access from the internet is impossible — all traffic must go through Caddy.
- **Token validation at the edge**: Caddy validates the auth token before proxying. Without a valid token (in URL or cookie), requests get a 401.
- **Trusted proxy headers**: OpenClaw only accepts `X-Forwarded-User` from Docker network IPs (`172.16.0.0/12`). An attacker cannot send this header directly because they can't reach port 18789.
- **Firewall**: UFW blocks all ports except 22 (SSH), 80 (HTTP redirect), and 443 (HTTPS).
- **No token in proxy**: Earlier approaches injected the auth token at the Caddy layer for all requests. This was a security flaw — anyone with the IP could access the dashboard. The current approach requires the token in the initial URL.

### Self-Signed TLS Certificate

Generated at boot time with the server's IP as the SAN:

```bash
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
    -keyout /opt/openclaw/certs/key.pem \
    -out /opt/openclaw/certs/cert.pem \
    -subj "/CN=${SERVER_IP}" \
    -addext "subjectAltName=IP:${SERVER_IP}"
```

Users see a browser warning on first visit ("Your connection is not private") and must click "Advanced → Proceed." This is unavoidable without a domain name and Let's Encrypt. The cert is valid for 10 years.

### Common Errors Reference

| Error | Cause | Fix |
|-------|-------|-----|
| `ERR_SSL_PROTOCOL_ERROR` | Caddy's `tls internal` doesn't work well for bare IPs | Use self-signed cert generated with `openssl` |
| `control ui requires device identity` | Plain HTTP doesn't provide secure context | Use HTTPS (self-signed cert) |
| `origin not allowed` | Control UI rejects non-whitelisted origins | Set `controlUi.allowedOrigins: ["*"]` |
| `pairing required` | OpenClaw requires device pairing for non-local access | Use `auth.mode: "trusted-proxy"` to bypass pairing |
| `gateway token missing` | WebSocket can't carry custom HTTP headers | Use cookie-based auth instead of header/query token |
| `requires gateway.trustedProxies` | Missing IP allowlist for proxy sources | Add `trustedProxies` at the `gateway` level (plural) |
| `set gateway.auth.trustedProxy` | Missing header config for trusted-proxy mode | Add `trustedProxy.userHeader` inside `auth` (singular) |
| `Proxy headers detected from untrusted address` | Caddy's Docker IP not in `trustedProxies` | Include Docker bridge CIDRs: `172.16.0.0/12`, `10.0.0.0/8` |
| Black/empty page | Static assets blocked by auth | Add `@static` path matcher for `/assets/*` |
| `disconnected (1006)` | WebSocket upgrade blocked by auth | Use cookie-based auth (auto-sent on WS upgrade) |

## Environments

| | Dev | Prod |
|---|---|---|
| Frontend | [dev.tardi.pages.dev](https://dev.tardi.pages.dev) | [tardi.pages.dev](https://tardi.pages.dev) |
| Backend | Cloud Run `tardi-api-dev` | Cloud Run `tardi-api-prod` |
| Database | Cloud SQL `tardi-db-dev` (f1-micro) | Cloud SQL `tardi-db-prod` (custom, HA) |
| Image tag | `dev-{sha7}` / `latest` | `prod-{sha7}` / `stable` |
