# CLAUDE.md

## Project Overview

Tardi is a dedicated AI agent hosting platform. Users configure an AI agent, subscribe to a $29/mo plan, and get a dedicated VPS provisioned automatically.

## Architecture

- **1 user = 1 agent** (no multi-agent support in this phase)
- **Single plan**: $29/month Standard tier
- **Mock mode**: Both auth (`USE_MOCK_AUTH` in `stores/auth.ts`) and API (`USE_MOCK` in `api/client.ts`) use mock data by default

## Frontend

- SvelteKit 2 with Svelte 5 runes (`$state`, `$derived`, `$props`)
- Tailwind CSS 4 (via Vite plugin, no config file)
- Adapter: Cloudflare Pages (`@sveltejs/adapter-cloudflare`)
- TypeScript strict mode

### Key Paths

- `frontend/src/lib/types/index.ts` — All shared TypeScript types
- `frontend/src/lib/api/mock.ts` — Mock data (single plan, single instance, snapshots)
- `frontend/src/lib/api/client.ts` — API client with `USE_MOCK` flag
- `frontend/src/lib/stores/auth.ts` — Auth store with `USE_MOCK_AUTH` flag
- `frontend/src/lib/stores/dashboard.ts` — Dashboard state with polling
- `frontend/src/lib/stores/onboarding.ts` — Onboarding flow state
- `frontend/src/routes/` — SvelteKit file-based routing

### Commands

```bash
cd frontend
npm run dev          # Dev server on :5173
npm run build        # Production build
npm run check        # Type check
```

Always commit and push to dev branch after any changes to code

### Conventions

- UI labels say "Agent" (not "Instance") for user-facing text
- Internal code still uses `instance`/`VpsInstance` types
- Gray-900 is the primary brand color (buttons, text, borders)
- No VPS specs (vCPU, RAM) shown to users — abstracted away
- All TODO backend integrations show `alert()` placeholder or simulate with `setTimeout`

## Backend

- Go 1.26 with `cmd/` and `internal/` layout
- PostgreSQL via Docker Compose (local), Cloud SQL (deployed)
- Migrations in `backend/migrations/` (Goose)
- Dockerfile: multi-stage alpine build, exposes 8080
- Deployed to GCP Cloud Run (`tardi-api-dev` / `tardi-api-prod`)

### Key Paths

- `backend/cmd/server/main.go` — Entry point
- `backend/internal/api/` — HTTP handlers and middleware
- `backend/internal/config/config.go` — Environment-based config
- `backend/internal/db/` — PostgreSQL connection, queries, migrations
- `backend/internal/provider/` — Multi-provider abstraction (Hetzner, etc.)
- `backend/internal/jobs/` — Async provisioning worker + reconciler
- `backend/Dockerfile` — Production container image
- `backend/Makefile` — `make build`, `make test`, `make lint`, `make dev`

### Commands

```bash
cd backend
make dev             # Hot reload with air
make build           # Compile to bin/server
make test            # go test ./... -v -race
make lint            # golangci-lint
make db-reset        # Reset local PostgreSQL
```

## Infrastructure

- Terraform in `infra/` targeting GCP
- Separate root per GCP project (blast-radius isolation)
- Dev project: `tardi-dev-488420`, Prod project: `tardi-prod-488420`
- State backends: `tardi-dev-488420-terraform-state` / `tardi-prod-488420-terraform-state`
- Shared reusable module: `infra/modules/backend-env/`

### Key Paths

- `infra/environments/dev/` — Dev root (main.tf, variables.tf, terraform.tfvars)
- `infra/environments/prod/` — Prod root (main.tf, variables.tf, terraform.tfvars)
- `infra/modules/backend-env/` — Reusable env module (Cloud Run, Cloud SQL, VPC, AR, Secrets, IAM, Monitoring)

## OpenClaw (Agent Runtime)

OpenClaw is the AI agent runtime that runs on each user's VPS inside a Docker container.

### Key Behaviors

- **OpenClaw owns `openclaw.json`** — it overwrites the file on startup with its internal config. Do NOT rely on editing this file externally; changes will be lost.
- **Config changes** must go through OpenClaw's `config.patch` WebSocket RPC (see `whatsapp.go` for examples), or be set before the very first boot.
- **Telegram bot token** is passed via `TELEGRAM_BOT_TOKEN` env var. OpenClaw auto-detects it and configures the Telegram channel automatically.

### Gateway Auth — Token Mode

The gateway uses `auth.mode: "token"` with `OPENCLAW_GATEWAY_TOKEN` env var. This was chosen over two alternatives that don't work:

- `auth.mode: "none"` — **crashes**: OpenClaw refuses to start with "Refusing to bind gateway to lan without auth" when `bind: "lan"` is set
- `auth.mode: "trusted-proxy"` — **breaks internal tool calls**: requires `X-Forwarded-User` header from a reverse proxy, but when the agent calls tools internally (sessions_list, browser, etc.) the calls go directly to `ws://127.0.0.1:18789` bypassing Caddy — no header, no auth, unauthorized

**How token mode works:**

```
External (browser → Caddy → OpenClaw):
  1. User visits https://<domain>/?token=<OPENCLAW_AUTH_TOKEN>
  2. Caddy validates token, sets oc_sess cookie
  3. Caddy proxies to OpenClaw with: Authorization: Bearer <OPENCLAW_AUTH_TOKEN>
  4. OpenClaw validates against OPENCLAW_GATEWAY_TOKEN env var → authenticated

Internal (agent tools → gateway):
  1. Agent calls ws://127.0.0.1:18789 for tool execution
  2. OpenClaw authenticates itself using its own OPENCLAW_GATEWAY_TOKEN → authenticated
```

**Two tokens, same value:**
- `OPENCLAW_AUTH_TOKEN` — used by Caddy for user-facing auth (cookie/query param validation) and passed to OpenClaw via `Authorization: Bearer` header
- `OPENCLAW_GATEWAY_TOKEN` — read by OpenClaw for gateway token auth. Same value as `OPENCLAW_AUTH_TOKEN`

**Config in `openclaw.json`:**
```json
{
  "gateway": {
    "bind": "lan",
    "auth": { "mode": "token" }
  }
}
```

**Drift guard** (`heartbeat.go`): Runs every 5 minutes. If OpenClaw reverts auth mode on restart, the drift guard patches `openclaw.json` back to `token` mode, migrates Caddy's `X-Forwarded-User` header to `Authorization: Bearer` (one-time), and ensures `OPENCLAW_GATEWAY_TOKEN` is in `.env`.

### Telegram Config Issues — Root Causes & Fixes

When OpenClaw auto-detects `TELEGRAM_BOT_TOKEN` from the env var, it creates a Telegram channel with **bad defaults** that cause two issues:

1. **Double replies**: Default `streaming: "partial"` sends an initial streaming chunk as one message, then the full response as a second message.
2. **Pairing prompt required**: Default `dmPolicy` is not `"open"`, so users see "access not configured" and must manually run a pairing command.

**Required Telegram channel settings** (applied post-startup via CLI or RPC):
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

### Telegram Config Sync Flow

The config is applied through **two mechanisms** that work together:

1. **SSH sync script** (`backend/internal/api/sync.go` — `configSyncScript`): Triggered when user enters bot token in dashboard. Recreates the container, waits for health, then applies config via `openclaw config set` CLI commands.
2. **Cleanup RPC** (`backend/internal/api/telegram.go` — `patchTelegramConfig`): Called by frontend after sync completes. Uses WebSocket `config.patch` RPC as a safety net to ensure the same settings.

**Critical ordering**: The sync script MUST print `"config sync complete"` only AFTER the Telegram config CLI commands have run. The frontend polls for this message to detect completion. If it appears before config is applied, the frontend shows success prematurely and the user messages the bot before `dmPolicy:"open"` is set — resulting in the pairing prompt.

**Previous bug (fixed 2026-03-17)**: The sync script printed completion BEFORE waiting for health and applying config. The frontend detected early completion, called the cleanup RPC (which failed since the container wasn't ready), and showed success. The user saw "Telegram bot connected" but the bot still required pairing because `dmPolicy:"open"` was never applied.

### Telegram Config — Important Notes

- Do NOT include `channels.telegram` in the cloud-init `openclaw.json` template — OpenClaw auto-detects from `TELEGRAM_BOT_TOKEN` env var
- OpenClaw's auto-detection defaults to `streaming: "partial"` and a restrictive `dmPolicy` — both must be overridden post-startup
- `dmPolicy: "open"` requires `allowFrom: ["*"]` — omitting `allowFrom` causes a config validation error and crash loop
- `allowFrom` must be set BEFORE `dmPolicy` when using sequential CLI commands (validation order dependency)
- The `config.patch` RPC uses WebSocket protocol (see `openclawRPC` in `whatsapp.go`)
- The heartbeat script (`provisioner.go`) has its own config sync section that correctly applies config before writing the version file — this was not affected by the bug

### Debugging OpenClaw on VPS

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

## CI/CD

GitHub Actions with two branches as source of truth:

- **`dev` branch** → development environment
- **`main` branch** → production environment

### Workflows (`.github/workflows/`)

| File                  | Trigger                              | Purpose                                                                                |
| --------------------- | ------------------------------------ | -------------------------------------------------------------------------------------- |
| `ci-gate.yml`         | PR to `dev`/`main`                   | Path-based change detection, calls reusable CI workflows, single required status check |
| `ci-frontend.yml`     | Reusable + PR                        | `npm run check` + `npm run build`                                                      |
| `ci-backend.yml`      | Reusable + PR                        | `golangci-lint` + `go test` + `go build` (3 parallel jobs)                             |
| `ci-infra.yml`        | Reusable + PR                        | `terraform fmt -check` + `validate` + `plan` (posts plan as PR comment)                |
| `deploy-frontend.yml` | Push to `dev`/`main` (frontend/\*\*) | Build + Wrangler deploy to Cloudflare Pages                                            |
| `deploy-backend.yml`  | Push to `dev`/`main` (backend/\*\*)  | Docker build → Artifact Registry → Cloud Run deploy                                    |
| `deploy-infra.yml`    | Push to `main` only (infra/\*\*)     | `terraform apply` (dev then prod, separate roots)                                      |

### Branch-to-Environment Mapping

| Branch | Frontend                | Backend        | Image Tags               |
| ------ | ----------------------- | -------------- | ------------------------ |
| `dev`  | dev.tardi-467.pages.dev | tardi-api-dev  | `dev-{sha7}` + `latest`  |
| `main` | app.tardi.ai            | tardi-api-prod | `prod-{sha7}` + `stable` |

### Key Design Decisions

- **CI Gate pattern**: `dorny/paths-filter` detects changes, conditionally runs component CI workflows. Single `gate` job is the only required status check (solves path-filter + required-check incompatibility)
- **Infra applies from `main` only**: Separate Terraform roots per project, applied sequentially (dev then prod)
- **GCP auth**: Workload Identity Federation (no long-lived service account keys)
- **Concurrency**: Per-branch groups with `cancel-in-progress: false` (running deploys finish)
- **Runtime vars** (`COMING_SOON`, `API_URL`): Set in `wrangler.toml` per environment, can be overridden in Cloudflare dashboard

### GitHub Secrets Required

- `GCP_PROJECT_ID`, `GCP_REGION`, `GCP_WORKLOAD_IDENTITY_PROVIDER`, `GCP_SERVICE_ACCOUNT`
- `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`
- `VITE_FIREBASE_API_KEY`, `VITE_FIREBASE_AUTH_DOMAIN`, `VITE_FIREBASE_PROJECT_ID`, `VITE_FIREBASE_STORAGE_BUCKET`, `VITE_FIREBASE_MESSAGING_SENDER_ID`, `VITE_FIREBASE_APP_ID`
