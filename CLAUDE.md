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

## Backend (WIP)

- Go with `cmd/` and `internal/` layout
- PostgreSQL via Docker Compose
- Migrations in `backend/migrations/`

## Infra (WIP)

- Terraform in `infra/`
- Hetzner Cloud provider for VPS provisioning
