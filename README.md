# Tardi

Dedicated AI agent hosting platform. Configure your agent, subscribe, and we handle the infrastructure.

## Stack

- **Frontend**: SvelteKit 2 + Svelte 5, Tailwind CSS 4, deployed on Cloudflare Pages
- **Backend**: Go (planned), PostgreSQL
- **Auth**: Firebase Authentication
- **Payments**: Stripe (planned)
- **Infra**: Hetzner VPS provisioning via Terraform (planned)

## Project Structure

```
frontend/       SvelteKit app (dashboard, onboarding, landing page)
backend/        Go API server (WIP)
infra/          Terraform configs (WIP)
docker-compose.yml   Local Postgres for development
```

## Getting Started

### Frontend

```bash
cd frontend
npm install
npm run dev
```

The app runs at `http://localhost:5173`. Mock auth is enabled by default — sign in with any email/password.

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
VITE_API_URL=http://localhost:8080
```

## Current State

The frontend is functional with mock data:

- Landing page with pricing ($29/mo single plan)
- Signup/login with mock auth (any email/password works)
- Onboarding flow: configure agent → review plan → checkout
- Dashboard: agent status, billing with cancel renewal, settings
- Agent detail: snapshots (take/restore), WhatsApp & Telegram bot links

Backend and infra are scaffolded but not yet implemented.
