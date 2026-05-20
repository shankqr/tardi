# CLAUDE.md

## Project Overview

Tardi is a dedicated AI agent hosting platform. Users configure an AI agent, subscribe, and get a dedicated VPS provisioned automatically.

## Architecture

- **1 user = 1 agent** (no multi-agent support in this phase)
- **Two plans**:
  - **Standard** — $29/mo, shared cloud infra
  - **Pro** — $65/mo, dedicated CPU
  - Plan tier is read from each Stripe price's `plan_tier` metadata (`standard` | `pro`) in `backend/internal/billing/stripe.go`.
  - Upgrade Standard → Pro is snapshot-based and preserves data (`backend/internal/jobs/upgrader.go` `executeUpgrade`).
  - Downgrade Pro → Standard re-provisions on the lower tier — **all agent data is lost** (`executeDowngrade`).
- **Mock mode**: Both auth (`USE_MOCK_AUTH` in `stores/auth.ts`) and API (`USE_MOCK` in `api/client.ts`) use mock data by default

## Frontend

- SvelteKit 2 with Svelte 5 runes (`$state`, `$derived`, `$props`)
- Tailwind CSS 4 (via Vite plugin, no config file)
- Adapter: Cloudflare Pages (`@sveltejs/adapter-cloudflare`)
- TypeScript strict mode

```bash
cd frontend
npm run dev          # Dev server on :5173
npm run build        # Production build
npm run check        # Type check
```

### Conventions

- UI labels say "Agent" (not "Instance") — internal code uses `instance`/`VpsInstance`
- Gray-900 is the primary brand color
- No VPS specs shown to users — abstracted away
- Default branch for code changes is `main` — commit and push directly to main. Only use the `dev` branch when spinning the dev environment back up.
- After code changes, automatically push to `main` and deploy the affected production service(s) from the laptop unless the user explicitly asks not to. Backend changes require the prod Cloud Run deploy command; frontend changes require `npm run deploy:prod`; full-stack changes require both. Docs-only or instruction-only changes do not require a prod deploy.

## Backend

- Go 1.26 with `cmd/` and `internal/` layout
- PostgreSQL via Docker Compose (local), Cloud SQL (deployed)
- Migrations in `backend/migrations/` (Goose)
- Deployed to GCP Cloud Run (`tardi-api-dev` / `tardi-api-prod`)

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
- Dev: `tardi-dev-488420`, Prod: `tardi-prod-2026`
- Shared module: `infra/modules/backend-env/`

### Cost controls

- Dev can be fully torn down and rebuilt on demand via `scripts/dev-teardown.sh` and `scripts/dev-bringup.sh`. Residual cost while torn down: ~$0.07/mo. See `docs/dev-wind-down.md`.

## CI/CD

- `dev` branch → dev environment, `main` branch → production
- Deploys auto-trigger on push (path-filtered per component)
- See `docs/cicd.md` for workflow details

## OpenClaw (Agent Runtime) — Critical Rules

OpenClaw runs on each user's VPS inside a Docker container. See `docs/openclaw-integration.md` for full details.

**Must-know rules when writing code that touches OpenClaw:**

- **OpenClaw owns `openclaw.json`** — it overwrites on startup. Config changes must go through `config.patch` WebSocket RPC or be set before first boot.
- **`config.patch` format**: `{raw: "<JSON string>", baseHash: "<from config.get>"}`. NOT `hash`, NOT direct config. Always call `config.get` first.
- **Two tokens, same value**: `OPENCLAW_AUTH_TOKEN` (DB/frontend) and `OPENCLAW_GATEWAY_TOKEN` (OpenClaw env var).
- **Dashboard URL**: `https://<domain>/#token=<TOKEN>` — hash fragment is the ONLY working auth delivery method.
- **Telegram config**: Do NOT put `channels.telegram` in cloud-init template. Override post-startup with `streaming: "off"` and `dmPolicy: "open"` (requires `allowFrom: ["*"]`). Set `allowFrom` BEFORE `dmPolicy`.
