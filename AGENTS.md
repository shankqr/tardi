# AGENTS.md

## Project Overview

Tardi is a dedicated AI agent hosting platform. Users configure an AI agent, subscribe, and get a dedicated VPS provisioned automatically.

## Architecture

- **1 user = 1 agent** (no multi-agent support in this phase)
- **Two plans**:
  - **Standard** — $29/mo, shared cloud infra
  - **Pro** — $65/mo, dedicated CPU
  - Plan tier is resolved from the Stripe price's `plan_tier` metadata (`standard` | `pro`); see [backend/internal/billing/stripe.go](backend/internal/billing/stripe.go).
  - Upgrade Standard → Pro is snapshot-based and preserves data ([backend/internal/jobs/upgrader.go](backend/internal/jobs/upgrader.go) `executeUpgrade`).
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

- **Old GCP billing is zeroed out** (as of 2026-04-29). `tardi-dev-488420` and `tardi-prod-488420` are unlinked from the old billing account `017711-880A01-FCEF09` (`billingEnabled: false`). Old prod's Cloud Run, Cloud SQL, Cloud Scheduler job, Cloud Run Job, Artifact Registry repos, Secret Manager secrets, and old GCS buckets were deleted. The old prod project remains only because frontend Firebase Auth still uses `tardi-prod-488420`.
- **Dev is fully torn down** (as of 2026-04-29). `tardi-api-dev` does not exist; the URL in [frontend/wrangler.toml](frontend/wrangler.toml) `[env.preview.vars]` returns 404. The old dev Terraform state buckets were deleted and billing is disabled, so bringup now requires relinking billing and recreating bootstrap state before `scripts/dev-bringup.sh` can work.
- To bring dev back up: relink billing for `tardi-dev-488420` from a gcloud config authenticated as the owner of that project (NOT the prod-2026 account), recreate the Terraform state bucket expected by [infra/environments/dev/main.tf](infra/environments/dev/main.tf), then run `scripts/dev-bringup.sh`. After bringup, refresh `API_URL` in `[env.preview.vars]` with the new Cloud Run hash.
- See [docs/dev-wind-down.md](docs/dev-wind-down.md) for the teardown/bringup procedure.

## CI/CD

**GitHub Actions is on hold** — the spending limit is $0 and will not be raised. Workflows still exist in [.github/workflows/](.github/workflows/) and remain runnable via `workflow_dispatch` if billing is ever restored, but **nothing auto-runs**:

- Push to `main` does NOT deploy anything.
- Push to `dev` does NOT deploy anything.
- PRs do NOT run CI (lint/test/typecheck).
- Scheduled jobs (synthetic monitor, prod E2E, sweeper) are commented out in the workflow files.

Basic prod uptime monitoring (curl + SSL) still runs on **GCP Cloud Scheduler → Cloud Run Job** — see [infra/modules/backend-env/synthetic_monitor.tf](infra/modules/backend-env/synthetic_monitor.tf). That's unaffected.

### Manual ops (while GH Actions is on hold)

Run all of these from the laptop. Active gcloud config must match the target project (`tardi-dev-488420` or `tardi-prod-2026`).

| Task | Command |
|---|---|
| Deploy backend (prod) | `cd backend && docker build -t us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest . && docker push ... && gcloud run services update tardi-api-prod --region=us-central1 --project=tardi-prod-2026 --image=us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest` |
| Deploy frontend (prod) | `cd frontend && npm run deploy:prod` |
| Deploy frontend (dev) | `cd frontend && npm run deploy:dev` |
| Apply infra changes | `cd infra/environments/{dev,prod} && terraform apply` |
| Sweep leftover prod E2E VPSes | `cd frontend && npx tsx e2e/scripts/cleanup-prod.ts` (needs `E2E_API_URL`, `FIREBASE_API_KEY`, `E2E_PROD_EMAIL`, `E2E_PROD_PASSWORD` from [frontend/.env.e2e.prod](frontend/.env.e2e.prod)) |
| Run prod E2E test | `cd frontend && npx playwright test --project=prod-e2e` (uses same env file) |
| Run synthetic monitor (Playwright deep check) | `cd frontend && npx playwright test --project=smoke` |
| Force-run GCP-side synthetic monitor | `gcloud scheduler jobs run tardi-synthetic-monitor --project=tardi-prod-2026 --location=us-central1` |

Sweep at least weekly to avoid leaking paid Hetzner VPSes from manual prod E2E runs.

See [docs/cicd.md](docs/cicd.md) for the original workflow design (kept for reference).

## OpenClaw (Agent Runtime) — Critical Rules

OpenClaw runs on each user's VPS inside a Docker container. See `docs/openclaw-integration.md` for full details.

**Must-know rules when writing code that touches OpenClaw:**

- **OpenClaw owns `openclaw.json`** — it overwrites on startup. Config changes must go through `config.patch` WebSocket RPC or be set before first boot.
- **`config.patch` format**: `{raw: "<JSON string>", baseHash: "<from config.get>"}`. NOT `hash`, NOT direct config. Always call `config.get` first.
- **Two tokens, same value**: `OPENCLAW_AUTH_TOKEN` (DB/frontend) and `OPENCLAW_GATEWAY_TOKEN` (OpenClaw env var).
- **Dashboard URL**: `https://<domain>/#token=<TOKEN>` — hash fragment is the ONLY working auth delivery method.
- **Telegram config**: Do NOT put `channels.telegram` in cloud-init template. Override post-startup with `streaming: "off"` and `dmPolicy: "open"` (requires `allowFrom: ["*"]`). Set `allowFrom` BEFORE `dmPolicy`.
