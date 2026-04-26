# Prod GCP Migration Runbook — `tardi-prod-488420` → `tardi-prod-2026`

**Status:** Phases A & B done; cutover in progress. New Cloud Run URL: `https://tardi-api-prod-loy7nru5uq-uc.a.run.app`. **A4 SKIPPED — reusing old Firebase from `tardi-prod-488420`** (decision 2026-04-26: backend only calls `VerifyIDToken` which is cross-project safe; old project must stay alive in Phase F). Active gcloud configuration: `tardi-prod-2026` (account `gigachadtrader69@gmail.com`); old account stays on `default` config.

When resuming, update the **Status** line above and the per-phase checkboxes so progress is visible at a glance.

## Context

Free credits on the current GCP prod project (`tardi-prod-488420`) are running out. We rebuild prod from scratch in a new GCP project (`tardi-prod-2026`) under a different GCP account that still has free credits. Old prod keeps serving until a single short cutover; old project is destroyed only after a 7-day soak.

**Decisions locked at planning time:**
- New project ID: `tardi-prod-2026`, region `us-central1` (unchanged)
- No existing prod users → fresh Firebase project, fresh DB, no data migration
- All existing prod-side VPSes will be force-destroyed in Hetzner (no per-VPS secret migration)
- API moves to a new permanent custom domain `api.tardi.ai` (Cloudflare-proxied → Cloud Run) so future GCP migrations don't require code edits
- Bootstrap (project create + billing link + Workload Identity Federation + Firebase) is part of the runbook

## Strategy

1. Build new project in parallel — old prod stays untouched.
2. Code PR is prepared but only merged inside the cutover window.
3. Cutover = add `api.tardi.ai` DNS + flip GitHub Actions secrets + merge PR. Reversible in ~5 min by deleting the DNS record and reverting the PR.
4. Old project is destroyed only after a 7-day soak.

---

## Phase A — Bootstrap new GCP project (no impact on old prod)

- [x] **A1. Create project + billing + base APIs** — done 2026-04-26. Billing account `011A70-DCE3D2-3F9CCC`.
```bash
gcloud projects create tardi-prod-2026 --name="Tardi Prod 2026"
gcloud beta billing projects link tardi-prod-2026 --billing-account=<NEW_BILLING_ACCOUNT_ID>
gcloud config set project tardi-prod-2026
gcloud services enable cloudresourcemanager.googleapis.com iam.googleapis.com \
  iamcredentials.googleapis.com sts.googleapis.com serviceusage.googleapis.com \
  storage.googleapis.com
```
Remaining APIs (run, sql, secretmanager, etc.) are enabled by Terraform via `google_project_service.apis` in [infra/modules/backend-env/main.tf](../infra/modules/backend-env/main.tf).

- [x] **A2. Create Terraform state bucket (versioned)** — done 2026-04-26.
```bash
gcloud storage buckets create gs://tardi-prod-2026-terraform-state \
  --project=tardi-prod-2026 --location=US --uniform-bucket-level-access
gcloud storage buckets update gs://tardi-prod-2026-terraform-state --versioning
```

- [x] **A3. Workload Identity Federation for GitHub Actions** — done 2026-04-26.
  - SA: `github-actions@tardi-prod-2026.iam.gserviceaccount.com` (13 deployer roles granted)
  - WIF pool: `github`, provider: `github-actions` (matches old-prod naming, NOT `github-oidc` as originally drafted)
  - Project number: `651232426874`
  - **Phase E2 secret values (verbatim):**
    - `GCP_PROJECT_ID` = `tardi-prod-2026`
    - `GCP_WORKLOAD_IDENTITY_PROVIDER` = `projects/651232426874/locations/global/workloadIdentityPools/github/providers/github-actions`
    - `GCP_SERVICE_ACCOUNT` = `github-actions@tardi-prod-2026.iam.gserviceaccount.com`
    - `GCP_REGION` = `us-central1` (unchanged)

Branch/approval gating stays at the GitHub Environment level (`production` / `infrastructure-prod`), already wired in [.github/workflows/deploy-backend.yml](../.github/workflows/deploy-backend.yml) and [.github/workflows/deploy-infra.yml](../.github/workflows/deploy-infra.yml).

- [x] **A4. Firebase + Google OAuth — SKIPPED (reusing old prod Firebase).**
  - Decision 2026-04-26: keep existing Firebase under `tardi-prod-488420` because backend only calls `VerifyIDToken` (cross-project safe — fetches public JWKS, no IAM needed). Frontend Firebase config unchanged. Existing E2E user `clawmyway+prodtesting@gmail.com` continues to work.
  - **Implication for Phase F:** old project must stay alive indefinitely. F3 must NOT run `gcloud projects delete tardi-prod-488420` — only destroy non-Firebase resources (Cloud Run, Cloud SQL, VPC, Artifact Registry, Secret Manager, Cloud Scheduler, BigQuery). Project keeps billing account attached; Firebase Auth standard tier is free up to 50K MAU.

---

## Phase B — Terraform changes (branch `migrate/prod-2026`, manual apply)

- [ ] **B1. Edits**
  - [infra/environments/prod/main.tf](../infra/environments/prod/main.tf) line 5 — backend `bucket = "tardi-prod-2026-terraform-state"` (literal string, not variable; backend blocks don't accept vars)
  - [infra/environments/prod/terraform.tfvars](../infra/environments/prod/terraform.tfvars) — `project_id = "tardi-prod-2026"`, `api_url = "https://api.tardi.ai"`
  - [infra/modules/backend-env/synthetic_monitor.tf](../infra/modules/backend-env/synthetic_monitor.tf) — update bootstrap doc-comments (cosmetic)

- [ ] **B2. Custom domain — Cloudflare-only path** (do NOT use `google_cloud_run_domain_mapping`): in Cloudflare for `tardi.ai` add `api` CNAME → `tardi-api-prod-<NEW-HASH>-uc.a.run.app`, **proxied (orange cloud)**. Edge cert auto-issues. Reversible in 30 sec.

- [ ] **B3. Module audit** — resource names (`tardi-api-prod`, `tardi-db-prod`, secret IDs, service accounts) are all project-scoped; no parameterization needed.

- [ ] **B4. First apply — manual, from laptop, NOT via CI**
```bash
cd infra/environments/prod
terraform init       # fresh state in new bucket — DO NOT use -migrate-state
terraform plan       # ~50 creates
terraform apply
```
Cloud Run will be unhealthy until B/C complete — that's expected.

---

## Phase C — Populate Secret Manager

The 15 secrets defined in [infra/modules/backend-env/secrets.tf](../infra/modules/backend-env/secrets.tf) split three ways:

- [ ] **C1. Copy verbatim** (13 secrets — Firebase reuse pulls 3 more into this bucket; read from old project, write to new, never to disk):
`prod-stripe-secret-key`, `prod-stripe-webhook-secret`, `prod-hetzner-api-token`, `prod-cloudflare-api-token`, `prod-cloudflare-zone-id`, `prod-cloudflare-base-domain`, `prod-ssh-private-key`, `prod-admin-api-token`, `prod-terminal-ticket-secret`, `prod-synthetic-monitor-gh-token`, **`prod-firebase-project-id` (stays `tardi-prod-488420`)**, **`prod-google-oauth-client-id`**, **`prod-google-oauth-client-secret`**

- [ ] **C2. Regenerate** (1 secret — safe because new DB is empty, even though old Firebase users will JIT-create rows on first login):
  - `prod-token-encryption-key` → `openssl rand -base64 32`

- [ ] **C3. Skip** — `prod-database-url` is owned by Terraform (no `ignore_changes`); already populated by B4.

Verify: `gcloud secrets list --project=tardi-prod-2026` shows all 15; each `gcloud secrets versions access latest --secret=<name>` returns non-`PLACEHOLDER` content.

---

## Phase D — Code PR (`migrate/prod-2026-code`, merged inside cutover window)

- [ ] Apply edits below on branch `migrate/prod-2026-code`:

| File | Change |
|---|---|
| [frontend/wrangler.toml](../frontend/wrangler.toml) `[env.production.vars]` | `API_URL = "https://api.tardi.ai"` only — all 6 `VITE_FIREBASE_*` UNCHANGED (Firebase reused) |
| [.github/workflows/prod-e2e.yml](../.github/workflows/prod-e2e.yml) | `E2E_API_URL` → `https://api.tardi.ai` only; `FIREBASE_API_KEY` UNCHANGED |
| [.github/workflows/prod-e2e-sweeper.yml](../.github/workflows/prod-e2e-sweeper.yml) | `E2E_API_URL` → `https://api.tardi.ai` only; `FIREBASE_API_KEY` UNCHANGED |
| [backend/cmd/synthetic-monitor/main.go](../backend/cmd/synthetic-monitor/main.go) | default `apiURL` → `https://api.tardi.ai` |
| [backend/scripts/db-query.sh](../backend/scripts/db-query.sh) | `PROJECT="tardi-prod-2026"`, instance `tardi-prod-2026:us-central1:tardi-db-prod` |
| [frontend/.env.e2e.prod](../frontend/.env.e2e.prod) | `E2E_API_URL` → `https://api.tardi.ai` only; Firebase API key UNCHANGED |
| [docs/synthetic-monitoring.md](synthetic-monitoring.md) | project ID references |
| [CLAUDE.md](../CLAUDE.md) | project ID references |

**Sequencing rule:** Do NOT merge until inside the cutover window (E4). Freeze unrelated PRs to `main` during the window.

---

## Phase E — Cutover (~30 min window)

- [ ] **E1. Pre-flight on new project (no DNS swing yet)** — pull latest prod image from old Artifact Registry, retag, push to new, deploy to new Cloud Run:
```bash
docker pull us-central1-docker.pkg.dev/tardi-prod-488420/tardi/api:latest
docker tag  us-central1-docker.pkg.dev/tardi-prod-488420/tardi/api:latest \
            us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest
docker push us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest
gcloud run services update tardi-api-prod --region=us-central1 --project=tardi-prod-2026 \
  --image=us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest
```
Verify: `curl https://tardi-api-prod-<NEW-HASH>-uc.a.run.app/readyz` → 200. Migrations run on boot against fresh DB; check Cloud Run logs.

- [ ] **E2. Update GitHub repo Environment secrets** (do this within the same minute as E4):
  - `production`: `GCP_PROJECT_ID=tardi-prod-2026`, `GCP_WORKLOAD_IDENTITY_PROVIDER=projects/651232426874/locations/global/workloadIdentityPools/github/providers/github-actions`, `GCP_SERVICE_ACCOUNT=github-actions@tardi-prod-2026.iam.gserviceaccount.com`. `GCP_REGION` unchanged.
  - `infrastructure-prod`: same three values.
  - `E2E_PROD_EMAIL` / `E2E_PROD_PASSWORD`: UNCHANGED (Firebase reused; existing test user still works).
  - Cloudflare / Sentry secrets unchanged.

- [ ] **E3. Add Cloudflare DNS** — `api.tardi.ai` CNAME → `tardi-api-prod-<NEW-HASH>-uc.a.run.app`, proxied. Verify `curl https://api.tardi.ai/readyz` → 200.

- [ ] **E4. Merge code PR** to `main`. This triggers `deploy-backend.yml` + `deploy-frontend.yml` against the new project. Watch both workflows green.

- [ ] **E5. Update Stripe webhook URL** in Stripe dashboard → `https://api.tardi.ai/webhooks/stripe` (verify exact path in `backend/internal/billing/`). Send test event, confirm 200.

- [ ] **E6. Verify synthetic monitor** — `gcloud scheduler jobs run tardi-synthetic-monitor --project=tardi-prod-2026 --location=us-central1`; check Cloud Run Job logs.

**Rollback** (any failure E3–E5): delete `api.tardi.ai` CNAME, revert PR, redeploy frontend. Old prod is intact. ~5 min recovery.

---

## Phase F — Soak + old project teardown

- [ ] **F1. Soak 7 days minimum** — covers weekly Cloud SQL maintenance and Stripe billing edges. Old prod keeps running as insurance.

- [ ] **F2. Hetzner cleanup (day 1 of soak)** — delete all old prod's Hetzner servers via console. Confirms the "no existing users" assumption.

- [ ] **F3. Teardown order (after soak):** — **MODIFIED for Firebase reuse: do NOT delete the project.** Old project must stay alive for Firebase Auth.
  1. `gcloud scheduler jobs pause tardi-synthetic-monitor --project=tardi-prod-488420 --location=us-central1` — watch 24h.
  2. Snapshot final state: `gcloud storage cp gs://tardi-prod-488420-terraform-state/terraform/state ./old-prod-final-state.tfstate`
  3. Disable Cloud SQL deletion protection: `gcloud sql instances patch tardi-db-prod --no-deletion-protection --project=tardi-prod-488420`
  4. From throwaway branch `teardown/prod-488420` (with old project_id and old backend bucket restored): `terraform destroy`. Tears down Cloud Run, Cloud SQL, VPC, Artifact Registry, Secret Manager, Cloud Scheduler, BigQuery, monitoring. **Firebase is not in TF — survives untouched.**
  5. `gcloud storage rm --recursive gs://tardi-prod-488420-terraform-state` (state bucket no longer needed; Firebase isn't TF-managed).
  6. ~~`gcloud projects delete tardi-prod-488420`~~ — **SKIPPED.** Project retains Firebase Auth + billing account attachment indefinitely. Verify monthly billing via `gcloud billing accounts get-iam-policy` is ~$0 (Firebase Auth free tier).

---

## Risk callouts

1. **Backend bucket is a literal string** in [infra/environments/prod/main.tf](../infra/environments/prod/main.tf) line 5. After B1 lands, anyone with a stale local checkout must `rm -rf .terraform/` before running TF, or risk pointing at the wrong state. Note this in the PR description.
2. **WIF + GitHub Environment race**: if E2 happens before E4, an unrelated push to `main` will deploy old code to new project. Mitigation: do E2 + E4 in tight sequence; freeze `main` during the cutover window.
3. **Cloud Run min_instance_count=1** in [infra/modules/backend-env/cloud_run.tf](../infra/modules/backend-env/cloud_run.tf) — billing starts the moment B4 applies. Verify free credits cover idle instance.
4. **OAuth consent screen verification** can take days if any sensitive scopes are in use. Start in Phase A1 — not at cutover.
5. **Billing export warmup** is ~24h before the new BigQuery dataset shows data — don't expect cost reports day 1.

---

## Critical files

- [infra/environments/prod/main.tf](../infra/environments/prod/main.tf) — backend bucket + module call
- [infra/environments/prod/terraform.tfvars](../infra/environments/prod/terraform.tfvars) — project_id, api_url
- [infra/modules/backend-env/secrets.tf](../infra/modules/backend-env/secrets.tf) — 15 secrets list
- [infra/modules/backend-env/main.tf](../infra/modules/backend-env/main.tf) — APIs, Cloud SQL, VPC
- [frontend/wrangler.toml](../frontend/wrangler.toml) — API_URL + Firebase config
- [.github/workflows/deploy-backend.yml](../.github/workflows/deploy-backend.yml), [.github/workflows/deploy-infra.yml](../.github/workflows/deploy-infra.yml), [.github/workflows/deploy-frontend.yml](../.github/workflows/deploy-frontend.yml)
- [.github/workflows/prod-e2e.yml](../.github/workflows/prod-e2e.yml), [.github/workflows/prod-e2e-sweeper.yml](../.github/workflows/prod-e2e-sweeper.yml)
- [backend/cmd/synthetic-monitor/main.go](../backend/cmd/synthetic-monitor/main.go)
- [backend/scripts/db-query.sh](../backend/scripts/db-query.sh)
- [frontend/.env.e2e.prod](../frontend/.env.e2e.prod)

---

## Verification (end-to-end)

After E6 completes, verify the full happy path on `https://app.tardi.ai`:
1. Fresh sign-up via Firebase — confirm new user lands in new Cloud SQL `users` table (`bash backend/scripts/db-query.sh prod "select count(*) from users"` after script edits).
2. API call from frontend hits `api.tardi.ai` (check browser devtools Network tab) and returns 200.
3. Stripe checkout flow — test card → confirm webhook hits `api.tardi.ai` (Stripe dashboard → recent webhooks → 200 status).
4. Provision a test agent — confirm Hetzner VPS spins up under new prod's Hetzner token, OpenClaw boots, dashboard loads.
5. Force-run synthetic monitor — `gcloud scheduler jobs run tardi-synthetic-monitor --project=tardi-prod-2026 --location=us-central1`; confirm green log.
6. Confirm CI: trigger a no-op `main` push — `deploy-backend.yml` and `deploy-infra.yml` both go green via WIF.
7. Confirm uptime checks fire from [infra/modules/backend-env/monitoring.tf](../infra/modules/backend-env/monitoring.tf) — check Cloud Monitoring → Uptime checks dashboard in new project.
