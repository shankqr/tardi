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

## Environments

| | Dev | Prod |
|---|---|---|
| Frontend | dev.tardi-467.pages.dev | app.tardi.ai |
| Backend | Cloud Run `tardi-api-dev` | Cloud Run `tardi-api-prod` |
| Database | Cloud SQL `tardi-db-dev` | Cloud SQL `tardi-db-prod` |

## Documentation

- [CI/CD](docs/cicd.md) — GitHub Actions workflows, branch-to-env mapping, secrets
- [OpenClaw Integration](docs/openclaw-integration.md) — Agent runtime auth, config, Telegram, debugging
- [VPS Architecture](docs/vps-architecture.md) — VPS provisioning and management
- [Config Sync](docs/config-sync-architecture.md) — How config changes propagate to VPSes
- [Security Audit](docs/security-audit.md) — Security review findings

## Key Tokens & Auth

| Token | Purpose |
|-------|---------|
| `AGENT_TOKEN` (64-char hex) | VPS → Backend API auth (heartbeat, config) |
| `OPENCLAW_AUTH_TOKEN` (64-char hex) | Dashboard → OpenClaw gateway auth |
| Root password (24-char hex) | Backend/User → VPS SSH |
| Firebase JWT | User → Backend API auth |
