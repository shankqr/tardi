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

## CI/CD

GitHub Actions with two branches as source of truth:

- **`dev` branch** → development environment
- **`main` branch** → production environment

### Workflows (`.github/workflows/`)

| File | Trigger | Purpose |
|------|---------|---------|
| `ci-gate.yml` | PR to `dev`/`main` | Path-based change detection, calls reusable CI workflows, single required status check |
| `ci-frontend.yml` | Reusable + PR | `npm run check` + `npm run build` |
| `ci-backend.yml` | Reusable + PR | `golangci-lint` + `go test` + `go build` (3 parallel jobs) |
| `ci-infra.yml` | Reusable + PR | `terraform fmt -check` + `validate` + `plan` (posts plan as PR comment) |
| `deploy-frontend.yml` | Push to `dev`/`main` (frontend/**) | Build + Wrangler deploy to Cloudflare Pages |
| `deploy-backend.yml` | Push to `dev`/`main` (backend/**) | Docker build → Artifact Registry → Cloud Run deploy |
| `deploy-infra.yml` | Push to `main` only (infra/**) | `terraform apply` (dev then prod, separate roots) |

### Branch-to-Environment Mapping

| Branch | Frontend | Backend | Image Tags |
|--------|----------|---------|------------|
| `dev` | dev.tardi-18e.pages.dev | tardi-api-dev | `dev-{sha7}` + `latest` |
| `main` | tardi-18e.pages.dev | tardi-api-prod | `prod-{sha7}` + `stable` |

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
