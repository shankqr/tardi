# Dedicated AI Agent Hosting — Architecture Specification

A multi-tenant dedicated AI Agent hosting platform that allows users to configure an OpenClaw AI agent, purchase a plan, and have infrastructure automatically provisioned, installed, and managed on their behalf.

The system is a **cloud orchestration control plane** specialized for AI agents. It translates user intent into automated infrastructure with a running intelligent workload.

---

## Technology Stack

| Layer | Technology |
|---|---|
| Frontend | SvelteKit on Cloudflare Pages |
| Backend control plane | Go services on Google Cloud Run |
| Database | PostgreSQL on Cloud SQL |
| Infrastructure providers | Hetzner, DigitalOcean, Contabo, Hostkey (via provider abstraction) |
| Payments | Stripe |
| Authentication | Firebase Authentication |
| Secrets | GCP Secret Manager |
| Observability | GCP Cloud Logging + Cloud Monitoring |
| IaC | Terraform |
| CI/CD | GitHub Actions |
| Realtime updates | Short polling (5s interval) |

---

## System Architecture

Four layers, top to bottom:

```
┌─────────────────────────────────────────────┐
│  Presentation Layer (SvelteKit / CF Pages)   │
├─────────────────────────────────────────────┤
│  Control Plane Layer (Go / Cloud Run)        │
├─────────────────────────────────────────────┤
│  Orchestration Layer (Go workers / Cloud Run)│
├─────────────────────────────────────────────┤
│  Infrastructure Layer (Hetzner / DO / etc.)  │
└─────────────────────────────────────────────┘
```

---

## 1. Presentation Layer

SvelteKit frontend served globally from Cloudflare Pages edge CDN.

**Responsibilities:**

- Marketing landing page
- Onboarding flow (agent config → plan selection → payment)
- Dashboard: VPS status, provisioning progress, agent health
- OpenClaw configuration interface
- Billing management

**Behavior:**

- Static frontend assets, no server-side rendering needed
- Authenticated REST calls to backend (Firebase JWT in `Authorization` header)
- Short polling (`GET /api/dashboard/state` every 5 seconds) for realtime updates
- No business logic — UI is a pure client of the control plane

---

## 2. Control Plane Layer

The central intelligence of the platform. Go services running as managed containers on Cloud Run.

**Responsibilities:**

- User authentication (Firebase JWT verification)
- Subscription management
- Stripe billing integration (webhooks)
- VPS lifecycle management (state machine)
- Provisioning job dispatch
- Provider selection (multi-provider abstraction)
- Agent heartbeat tracking
- REST API for dashboard and agent communication
- Audit logging

**How it works:**

```
user clicks "create agent"
  → control plane validates subscription limits
  → selects cheapest available provider for requested plan
  → creates provisioning job in database
  → worker picks up job and executes
  → dashboard polls for status updates
```

---

## 3. Orchestration Layer

Async worker services responsible for executing infrastructure tasks. Runs as a separate Cloud Run service (or goroutine within the same service for simplicity).

**Responsibilities:**

- Execute provisioning jobs from the PostgreSQL job queue
- Call provider APIs (Hetzner, DigitalOcean, Contabo, Hostkey)
- Create / destroy / start / stop servers
- Bootstrap OS via cloud-init
- Install OpenClaw agent
- Retry failed operations with exponential backoff
- Reconcile drift between database and actual infrastructure state

**This layer is stateless** — all state lives in PostgreSQL. Workers are interchangeable.

---

## 4. Infrastructure Layer

Resources created and managed on behalf of customers across multiple providers:

- Virtual machines (Hetzner, DigitalOcean, Contabo, Hostkey)
- Installed OpenClaw agent runtime
- Network resources (public IPv4)

The platform does not host workloads — it orchestrates them.

---

## Multi-Provider Abstraction

The architecture supports multiple infrastructure providers through a Go interface pattern.

### Provider Interface

```go
type InfraProvider interface {
    CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error)
    GetServer(ctx context.Context, providerServerID string) (*Server, error)
    StartServer(ctx context.Context, providerServerID string) error
    StopServer(ctx context.Context, providerServerID string) error
    DeleteServer(ctx context.Context, providerServerID string) error
}
```

All provider-specific details (server types, regions, OS images) are abstracted behind this interface.

### Provider Implementations

```
internal/
  provider/
    provider.go          # interface + normalized types
    registry.go          # provider registry + selection algorithm
    hetzner/
      hetzner.go         # InfraProvider using hcloud-go SDK
    digitalocean/
      digitalocean.go    # InfraProvider using godo SDK
    contabo/
      contabo.go         # InfraProvider using REST client
    hostkey/
      hostkey.go         # InfraProvider using REST client
```

### Normalized Plan Catalog

Plans are normalized across providers. Users see abstract tiers; the system maps to provider-specific server types.

User-facing tiers:

| Tier | Specs |
|---|---|
| Starter | 2 vCPU, 4 GB RAM, 1 VPS |
| Pro | 4 vCPU, 8 GB RAM, up to 3 VPS |
| Business | 8 vCPU, 16 GB RAM, up to 5 VPS |

Behind the scenes, a `provider_plan_mappings` table maps each tier to provider-specific server types:

| plan_tier | provider | region | provider_server_type | provider_region | monthly_cost_cents |
|---|---|---|---|---|---|
| starter | hetzner | eu-central | cx22 | fsn1 | 499 |
| starter | digitalocean | us-east | s-2vcpu-4gb | nyc1 | 2400 |
| starter | contabo | eu-central | VPS S SSD | EU | 599 |

This lives in the database, not code — providers/regions can be added without redeploying.

### Provider Selection Algorithm

System picks the provider automatically:

1. Filter `provider_plan_mappings` by requested `plan_tier` and closest `region`
2. Exclude unavailable providers (`is_available = false`)
3. Select **lowest cost** (maximum margin)
4. If provisioning fails → automatic fallback to next cheapest provider

### Build Priority

1. Ship with **Hetzner only** (interface exists from day one)
2. Add **DigitalOcean** second (best SDK quality)
3. Add **Contabo** and **Hostkey** later (custom REST clients)

---

## Authentication

Firebase Authentication — free tier, GCP-native.

- Frontend uses Firebase JS SDK for login (email/password + Google OAuth)
- Firebase issues JWTs automatically
- Go backend verifies Firebase JWTs via `firebase-admin-go` SDK (~10 lines of middleware)
- User record created in PostgreSQL on first verified request (upsert by Firebase UID)
- No session management, token refresh logic, or password hashing to build

**Authorization:** Resource ownership model — every DB query scoped by `user_id`. Middleware extracts user from JWT and injects into request context.

---

## Agent Communication Model

OpenClaw is the platform's own AI agent software, installed on customer VPS instances.

### Phone-Home Pattern

The agent communicates with the control plane by phoning home, not the other way around.

- Agent runs a lightweight HTTP client that polls the control plane every 30 seconds
- `GET /api/agent/config` — fetch latest configuration
- `POST /api/agent/heartbeat` — report health status and metrics
- Agent authenticates with a per-instance API token (generated during provisioning, stored in GCP Secret Manager)
- Control plane marks instance as `unhealthy` if no heartbeat received for 3 minutes

### Why Phone-Home (Not SSH)

SSH from control plane requires maintaining connections, firewall rules, and key rotation. Phone-home is simpler, works through NATs, and the agent already needs outbound internet access.

### Initial Provisioning

Initial setup uses Hetzner's cloud-init (user-data script) — a one-time bootstrap. After OpenClaw is installed and phones home for the first time, SSH access is no longer needed by the control plane.

---

## Async Job Processing

The orchestration layer uses a **PostgreSQL-backed job queue** — no external queue service.

### How It Works

- `provisioning_jobs` table with `SELECT ... FOR UPDATE SKIP LOCKED` for concurrent-safe polling
- A Go worker goroutine polls every 2 seconds
- Job states: `pending` → `running` → `completed` / `failed` / `dead`
- Failed jobs retry with **exponential backoff** (base 5s, factor 2x, max 5min, 5 attempts)
- After max retries → mark `dead`, alert via structured log, surface in dashboard
- All operations are **idempotent** — each job carries an idempotency key; provider API calls check for existing resources before creating

### Provisioning Steps

Each step is recorded in the job, making the process resumable:

1. `select_provider` — call `registry.SelectProvider(plan, region)` for best mapping
2. `create_server` — call provider API
3. `wait_server_ready` — poll until server status is `running`
4. `bootstrap` — cloud-init completes (verified by agent phone-home)
5. `install_agent` — agent starts and first heartbeat received
6. `activate` — mark VPS as `active` in database

### Per-Step Timeouts

| Step | Timeout |
|---|---|
| create_server | 5 minutes |
| wait_server_ready | 5 minutes |
| bootstrap | 10 minutes |
| install_agent | 10 minutes |

If a timeout is exceeded, the step is marked failed and retried.

### Reconciliation Loop

A periodic goroutine (every 10 minutes) queries all `active` VPS instances, cross-references the provider API, and flags drift (server deleted externally, IP changed, etc.). Logs discrepancies and updates the database.

---

## VPS Lifecycle State Machine

Every VPS moves through a deterministic state machine. Transitions are recorded and replayable.

```
requested → provisioning → bootstrapping → installing_agent → active

active ↔ restarting

active → suspending → suspended → resuming → active

suspended → terminating → terminated

active → error
```

| State | Meaning |
|---|---|
| `requested` | User created, awaiting provisioning |
| `provisioning` | Server being created on provider |
| `bootstrapping` | OS booting, cloud-init running |
| `installing_agent` | OpenClaw being installed |
| `active` | Running and healthy |
| `restarting` | User-initiated restart in progress |
| `suspending` | Billing failure — stopping VPS on provider |
| `suspended` | VPS stopped due to non-payment (not deleted) |
| `resuming` | Payment received — restarting VPS |
| `terminating` | VPS being destroyed on provider |
| `terminated` | VPS destroyed, data deleted |
| `error` | Recoverable failure — can retry or needs manual intervention |

---

## Billing Architecture

Stripe Checkout + Webhooks with clear lifecycle rules.

### Plan Tiers

Defined as Stripe Products:

- **Starter:** 1 VPS, basic agent config
- **Pro:** up to 3 VPS, advanced config, priority support
- More tiers added later as needed

### Purchase Flow

```
SvelteKit frontend
  → backend creates Stripe Checkout Session
  → user redirects to Stripe
  → payment processed
  → Stripe webhook: checkout.session.completed
  → backend creates subscription record + triggers provisioning
```

### Recurring Billing

Stripe handles invoicing automatically. Backend reacts to webhooks:

| Webhook | Action |
|---|---|
| `invoice.paid` | Ensure resources active |
| `invoice.payment_failed` | Mark subscription `past_due`, send warning |
| `customer.subscription.updated` | Adjust resource limits (proration) |
| `customer.subscription.deleted` | Begin teardown grace period |

### Grace Periods

- **7 days** after payment failure → VPS suspended (stopped, not deleted)
- **30 days** after suspension → VPS terminated (data deleted)

### Webhook Security

Verify Stripe webhook signatures on every request using Stripe's signing secret.

---

## Security Model

### Rate Limiting

Go middleware using `golang.org/x/time/rate`:
- 60 requests/min per user for general API
- 10 requests/min for provisioning actions

### CORS

Allow only the Cloudflare Pages domain. Configured in Go HTTP middleware.

### Input Validation

Validate all API inputs at the handler level using Go struct tags.

### Authorization

Resource ownership model — every database query scoped by `user_id`. No resource can be accessed without ownership validation.

### Provider API Isolation

Each customer's VPS is labeled with their `user_id` on the provider. Control plane queries are always scoped by label.

### Audit Logging

Significant actions written to an `audit_log` table:
- VPS create / delete / restart
- Configuration changes
- Billing events
- Login events

### Infrastructure Isolation

- Each customer VPS is independent
- Database on private network only
- Backend uses GCP service identity
- Frontend public but hardened (Cloudflare protection)

---

## Secrets Management

GCP Secret Manager for all credentials.

| Secret Type | Storage | Lifecycle |
|---|---|---|
| Platform secrets (Hetzner/DO/Contabo/Hostkey API tokens, Stripe keys, Firebase config) | Secret Manager → Cloud Run env vars | Deployed, rotated manually |
| Per-instance agent tokens | Secret Manager with `instance_id` label | Created during provisioning, deleted on termination |
| SSH keys for initial provisioning | Ephemeral | Generated per-provision, used once in cloud-init, discarded |
| Per-provider API credentials | Secret Manager | `hetzner-api-token`, `digitalocean-api-token`, `contabo-api-credentials`, `hostkey-api-key` |

---

## Database Schema

PostgreSQL is the **single source of truth**. Infrastructure is treated as external state that must match database records.

### Core Tables

```sql
users (
    id UUID PRIMARY KEY,
    firebase_uid TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

subscriptions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    stripe_subscription_id TEXT UNIQUE NOT NULL,
    stripe_customer_id TEXT NOT NULL,
    plan_tier TEXT NOT NULL,           -- "starter", "pro", "business"
    status TEXT NOT NULL,              -- "active", "past_due", "canceled", "suspended"
    current_period_end TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

vps_instances (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id),
    provider TEXT NOT NULL,            -- "hetzner", "digitalocean", "contabo", "hostkey"
    provider_server_id TEXT UNIQUE,    -- provider-specific server ID
    provider_region TEXT,              -- actual provider region used
    name TEXT NOT NULL,
    ipv4 INET,
    region TEXT NOT NULL,              -- normalized region
    status TEXT NOT NULL,              -- state machine value
    agent_token_secret_name TEXT,      -- reference to GCP Secret Manager
    last_heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

provisioning_jobs (
    id UUID PRIMARY KEY,
    vps_instance_id UUID NOT NULL REFERENCES vps_instances(id),
    idempotency_key TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL,              -- "pending", "running", "completed", "failed", "dead"
    step TEXT,                         -- current provisioning step
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_retry_at TIMESTAMPTZ,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

provider_plan_mappings (
    id UUID PRIMARY KEY,
    plan_tier TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,               -- normalized region
    provider_server_type TEXT NOT NULL,  -- e.g. "cx22", "s-2vcpu-4gb"
    provider_region TEXT NOT NULL,       -- e.g. "fsn1", "nyc1"
    provider_image TEXT NOT NULL,        -- OS image ID
    monthly_cost_cents INT NOT NULL,
    is_available BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

agent_configs (
    id UUID PRIMARY KEY,
    vps_instance_id UUID UNIQUE NOT NULL REFERENCES vps_instances(id),
    config JSONB NOT NULL,
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

audit_log (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
```

### Indexes

```sql
CREATE INDEX idx_provisioning_jobs_poll ON provisioning_jobs(status, next_retry_at);
CREATE INDEX idx_vps_instances_user ON vps_instances(user_id);
CREATE INDEX idx_audit_log_user_time ON audit_log(user_id, created_at);
CREATE INDEX idx_provider_plan_mappings_lookup ON provider_plan_mappings(plan_tier, region, is_available);
```

### Tooling

- **Migrations:** `goose` (Go-native, SQL-based migration files)
- **Connection pooling:** Cloud SQL Auth Proxy (built into Cloud Run) + `pgxpool` in Go
- **Driver:** `pgx` (fastest pure-Go PostgreSQL driver)

---

## Realtime Updates

Short polling — the simplest approach with zero infrastructure overhead.

- Frontend calls `GET /api/dashboard/state` every 5 seconds
- Firebase JWT in `Authorization` header for auth
- Returns current state of all user resources
- Zero persistent connections, zero server-side connection tracking
- Works perfectly with Cloud Run's request-based scaling model

**Response shape:**

```json
{
  "instances": [
    {
      "id": "uuid",
      "name": "my-agent",
      "status": "provisioning",
      "step": "create_server",
      "provider": "hetzner",
      "ipv4": null,
      "last_heartbeat_at": null
    }
  ],
  "subscription": {
    "plan": "starter",
    "status": "active",
    "current_period_end": "2026-03-21T00:00:00Z"
  },
  "pending_jobs": 1
}
```

5-second latency is acceptable — VPS provisioning takes minutes, agent heartbeats are every 30 seconds.

**Future upgrade path:** If sub-second updates are needed (live log streaming, web terminal), add Server-Sent Events (SSE) alongside polling.

---

## Observability

Lean on GCP's built-in stack — zero extra services to manage.

| Concern | Solution |
|---|---|
| Logging | Go `slog` (stdlib) with JSON output → Cloud Logging auto-capture |
| Metrics | Cloud Run built-in (request count, latency, instances) + custom metrics from DB queries |
| Health checks | `GET /healthz` (liveness), `GET /readyz` (readiness — DB connectivity) |
| Alerting | Cloud Monitoring policies: error log spike, health check failures, job queue depth > 20 |
| Dashboard | Single Cloud Monitoring dashboard: active instances, queue depth, error rate, p95 latency |

---

## Deployment Model

### Environments

Two environments, each tied to a Git branch:

| Environment | Branch | Frontend | Backend | Database |
|---|---|---|---|---|
| Development | `dev` | dev.tardi.pages.dev | Cloud Run `tardi-api-dev` | Cloud SQL `tardi-db-dev` (f1-micro, single zone) |
| Production | `main` | tardi.pages.dev | Cloud Run `tardi-api-prod` | Cloud SQL `tardi-db-prod` (custom-1-3840, regional HA) |

### Branching Strategy

```
feature branch → PR to dev → merge → deploys to dev environment
                               ↓
              PR from dev to main → merge → deploys to prod environment
```

Feature branches are created from `dev`. All changes flow through `dev` before reaching `main`.

### Infrastructure as Code

Terraform for GCP resources, stored in `infra/` directory:

```
infra/
  main.tf                  # Provider, APIs, module.dev + module.prod
  variables.tf             # Top-level variables
  network.tf               # VPC, private service networking
  artifact_registry.tf     # Docker image registry
  monitoring.tf            # Alert policies
  environments/
    dev.tfvars             # Dev overrides (f1-micro, latest tag)
    prod.tfvars            # Prod overrides (custom tier, stable tag)
  modules/
    backend-env/           # Reusable module per environment
      main.tf              # Cloud SQL instance
      cloud_run.tf         # Cloud Run service
      secrets.tf           # Secret Manager entries
      iam.tf               # Service account + roles
```

Single Terraform root module with both `module.dev` and `module.prod`. Shared root-level resources: VPC, Artifact Registry, API enablements, monitoring. State stored in GCS bucket (`tardi-terraform-state`).

---

## CI/CD Pipeline (GitHub Actions)

### Workflow Architecture

Seven workflow files in `.github/workflows/`:

```
.github/workflows/
  ci-gate.yml            # PR gate — change detection + orchestration
  ci-frontend.yml        # Reusable: type check + build
  ci-backend.yml         # Reusable: lint + test + build
  ci-infra.yml           # Reusable: terraform validate + plan
  deploy-frontend.yml    # CD: build + deploy to Cloudflare Pages
  deploy-backend.yml     # CD: Docker build → Artifact Registry → Cloud Run
  deploy-infra.yml       # CD: terraform apply (main branch only)
```

### CI: Pull Request Checks

Every PR to `dev` or `main` triggers the CI Gate workflow:

```
PR opened/updated
    │
    ▼
ci-gate.yml
    │
    ├── dorny/paths-filter (detect which dirs changed)
    │
    ├── frontend/**  → ci-frontend.yml
    │                   ├── npm run check (svelte-check + TypeScript)
    │                   └── npm run build (validates production build)
    │
    ├── backend/**   → ci-backend.yml
    │                   ├── golangci-lint (parallel)
    │                   ├── go test ./... -v -race (parallel)
    │                   └── CGO_ENABLED=0 go build (parallel)
    │
    ├── infra/**     → ci-infra.yml
    │                   ├── terraform fmt -check -recursive
    │                   ├── terraform init
    │                   ├── terraform validate
    │                   └── terraform plan (output posted as PR comment)
    │
    └── gate job (if: always())
        └── "All Checks Passed" — single required status check
```

**Why a CI Gate?** GitHub requires that required status checks actually report for every PR. Path-filtered workflows skip entirely when their paths don't change, leaving the check as "pending" forever. The gate pattern uses `dorny/paths-filter` to conditionally call reusable workflows, with a final `gate` job that always runs and reports success/failure.

### CD: Frontend Deployment

Triggered on push to `dev` or `main` when `frontend/**` changes:

```
Push to dev/main (frontend/** changed)
    │
    ▼
deploy-frontend.yml
    │
    ├── npm ci
    ├── npm run build (with real VITE_FIREBASE_* secrets)
    └── wrangler pages deploy --branch=${{ github.ref_name }}
        │
        ├── dev branch  → dev.tardi.pages.dev (preview environment)
        └── main branch → tardi.pages.dev (production)
```

Runtime variables (`COMING_SOON`, `API_URL`) are configured in the Cloudflare Pages dashboard, not baked into the build. They are read server-side via `platform.env`.

### CD: Backend Deployment

Triggered on push to `dev` or `main` when `backend/**` changes:

```
Push to dev/main (backend/** changed)
    │
    ▼
deploy-backend.yml
    │
    ├── Authenticate to GCP (Workload Identity Federation)
    ├── Login to Artifact Registry
    ├── Docker build (multi-stage, Buildx with GHA cache)
    ├── Push image with tags:
    │   ├── dev branch:  dev-{sha7} + latest
    │   └── main branch: prod-{sha7} + stable
    └── Deploy to Cloud Run
        ├── dev branch:  tardi-api-dev with image dev-{sha7}
        └── main branch: tardi-api-prod with image prod-{sha7}
```

Docker image tagging convention:
- **`{env}-{sha7}`**: Immutable tag for every build (e.g., `dev-a1b2c3d`). Used for actual Cloud Run deployments. Makes rollbacks trivial.
- **`latest` / `stable`**: Floating convenience tags. `latest` tracks dev HEAD, `stable` tracks prod HEAD.

### CD: Infrastructure Deployment

Triggered on push to `main` only when `infra/**` changes:

```
Push to main (infra/** changed)
    │
    ▼
deploy-infra.yml
    │
    ├── Authenticate to GCP (Workload Identity Federation)
    ├── terraform init
    └── terraform apply -var-file=environments/dev.tfvars -var-file=environments/prod.tfvars -auto-approve
```

**Infrastructure applies from `main` only.** Both dev and prod modules share Terraform state (VPC, Artifact Registry, API enablements). Splitting state would require a third "shared" root module — unnecessary at this scale. All infra changes go through PR review before reaching `main`.

### Concurrency Control

Each deploy workflow uses a concurrency group scoped by branch:

```yaml
concurrency:
  group: deploy-{component}-${{ github.ref_name }}
  cancel-in-progress: false
```

This prevents parallel deployments to the same environment. `cancel-in-progress: false` ensures running deployments finish rather than being killed by newer pushes.

### GCP Authentication: Workload Identity Federation

GitHub Actions authenticates to GCP via OIDC — no long-lived service account keys.

Setup:
1. Workload Identity Pool: `github-actions`
2. OIDC Provider: `github` (bound to `token.actions.githubusercontent.com`)
3. Attribute condition: scoped to the repository
4. Service account: `github-actions-cicd@{project}.iam.gserviceaccount.com`
5. Roles: `artifactregistry.writer`, `run.admin`, `iam.serviceAccountUser`, `storage.admin`, `editor`

### GitHub Secrets

| Secret | Purpose |
|---|---|
| `GCP_PROJECT_ID` | GCP project identifier |
| `GCP_REGION` | GCP region (e.g., `us-central1`) |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Pool provider path |
| `GCP_SERVICE_ACCOUNT` | CI/CD service account email |
| `CLOUDFLARE_API_TOKEN` | Wrangler deployment (Cloudflare Pages:Edit permission) |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account identifier |
| `VITE_FIREBASE_API_KEY` | Firebase web API key (build-time) |
| `VITE_FIREBASE_AUTH_DOMAIN` | Firebase auth domain (build-time) |
| `VITE_FIREBASE_PROJECT_ID` | Firebase project ID (build-time) |
| `VITE_FIREBASE_STORAGE_BUCKET` | Firebase storage bucket (build-time) |
| `VITE_FIREBASE_MESSAGING_SENDER_ID` | Firebase messaging sender ID (build-time) |
| `VITE_FIREBASE_APP_ID` | Firebase app ID (build-time) |

### GitHub Environments

Three GitHub Environments for deployment protection:

| Environment | Used By | Protection |
|---|---|---|
| `development` | deploy-frontend, deploy-backend (dev) | None |
| `production` | deploy-frontend, deploy-backend (main) | Optional: manual approval |
| `infrastructure` | deploy-infra | Optional: manual approval |

### Branch Protection Rules

**`main` branch:**
- Require PR reviews (1 approver)
- Require "CI Gate / All Checks Passed" status check
- Require linear history (rebase/squash merges)
- No direct push

**`dev` branch:**
- Require "CI Gate / All Checks Passed" status check
- PR reviews optional

### End-to-End Deployment Flow

```
Developer pushes feature branch
    │
    ▼
PR to dev ──── CI Gate runs relevant checks
    │              │
    │          All Checks Passed ✓
    │              │
    ▼              ▼
Merge to dev ─── Triggers deploy workflows (path-filtered)
    │              ├── deploy-frontend.yml → dev.tardi.pages.dev
    │              └── deploy-backend.yml  → tardi-api-dev
    │
    ▼
Manual testing on dev environment
    │
    ▼
PR from dev to main ── CI Gate runs all checks again
    │                       │
    │                   Review + Approve
    │                       │
    ▼                       ▼
Merge to main ──────── Triggers deploy workflows
                           ├── deploy-frontend.yml → tardi.pages.dev
                           ├── deploy-backend.yml  → tardi-api-prod
                           └── deploy-infra.yml    → terraform apply (if infra/** changed)
```

---

## User Experience Flows

### New User Flow

1. User visits landing page
2. Configures OpenClaw agent parameters
3. Selects subscription plan (Starter / Pro / Business)
4. Redirected to Stripe Checkout for payment
5. Payment processed → Stripe webhook fires
6. Account created, subscription activated
7. User enters dashboard
8. System selects cheapest provider, creates provisioning job
9. Dashboard polls for progress (provisioning → bootstrapping → installing → active)
10. Agent phones home, dashboard shows running instance
11. User manages configuration from dashboard

### Existing User Flow

1. User logs in via Firebase Auth
2. Dashboard loads, begins polling `GET /api/dashboard/state`
3. Current VPS state, agent health, and subscription status displayed
4. User manages configuration, restarts, or lifecycle actions

### Billing Failure Flow

1. Stripe `invoice.payment_failed` webhook received
2. Subscription marked `past_due`
3. 7 days grace period — user warned
4. After 7 days → VPS suspended (stopped on provider, not deleted)
5. User pays → VPS resumed automatically
6. After 30 days suspended → VPS terminated, data deleted

---

## Scalability Model

- Horizontal scaling via Cloud Run auto-scaling (stateless services)
- All state centralized in PostgreSQL
- Provisioning workers scale independently of API layer
- Short polling scales naturally with Cloud Run's request model
- Provider abstraction allows distributing load across multiple infrastructure providers

---

## Future Expansion Paths

Natural evolution of this architecture:

- Multi-region normalized regions across all providers
- Auto-scaling VPS fleets per customer
- Template marketplace for agent configurations
- Usage-based billing and metering
- Infrastructure metrics pipeline (agent telemetry)
- Live log streaming via SSE
- Web terminal via WebSockets
- Additional providers (Vultr, Linode, OVH)
- Customer-facing API with API keys
