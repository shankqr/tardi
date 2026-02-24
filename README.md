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

## Environments

| | Dev | Prod |
|---|---|---|
| Frontend | [dev.tardi.pages.dev](https://dev.tardi.pages.dev) | [tardi.pages.dev](https://tardi.pages.dev) |
| Backend | Cloud Run `tardi-api-dev` | Cloud Run `tardi-api-prod` |
| Database | Cloud SQL `tardi-db-dev` (f1-micro) | Cloud SQL `tardi-db-prod` (custom, HA) |
| Image tag | `dev-{sha7}` / `latest` | `prod-{sha7}` / `stable` |
