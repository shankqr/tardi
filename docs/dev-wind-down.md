# Dev environment wind-down

Two scripts let you tear down the dev GCP environment to near-zero idle cost
and rebuild it on demand when you need to test something.

## Scripts

| Script | Purpose |
|---|---|
| [scripts/dev-teardown.sh](../scripts/dev-teardown.sh) | Back up secrets, `terraform destroy` dev |
| [scripts/dev-bringup.sh](../scripts/dev-bringup.sh) | `terraform apply`, restore secrets, bounce Cloud Run |

## Cost

| State | Est. monthly |
|---|---|
| Running (current baseline) | ~$15–23 |
| Torn down | ~$0.07 (state bucket + preserved registry) |

## Tear down

```bash
./scripts/dev-teardown.sh            # prompts for confirmation
./scripts/dev-teardown.sh --yes      # no prompt
./scripts/dev-teardown.sh --dump-db  # also pg_dump the DB before destroy
```

What happens:

1. Dumps every `dev-*` Secret Manager value (except `dev-database-url`, which
   is regenerated on apply) into `scripts/.dev-secrets-backup/`. That dir is
   gitignored and `chmod 700`.
2. `terraform state rm` for resources we want to survive the destroy:
   - **Artifact Registry** `tardi` — so images for Cloud Run cold-start persist.
   - **VPC, subnet, reserved IP range, service networking peering** — GCP's
     service networking backend keeps the peering pinned for hours after
     Cloud SQL destroy (producer-service cleanup lag), making the TF
     destroy hang indefinitely. These resources cost $0/mo when idle.
3. `terraform destroy -auto-approve` in [infra/environments/dev/](../infra/environments/dev/).

What gets destroyed: Cloud SQL (and its data), Cloud Run, IAM service account,
logging bucket config, all 14 Secret Manager entries, API enablement.

What survives (state-rm'd first): Artifact Registry `tardi` (images), VPC,
subnet, reserved IP range, service networking peering, Terraform state bucket
`tardi-dev-488420-terraform-state`, the local backup directory.

**Do not push to the `dev` branch while dev is torn down.** `deploy-backend`
will fail — Cloud Run service is gone and migrations can't run. Frontend
pushes to Cloudflare Pages still work but the dashboard will 5xx on any API
call.

## Bring up

```bash
./scripts/dev-bringup.sh         # prompts for confirmation
./scripts/dev-bringup.sh --yes   # no prompt
```

What happens:

1. `terraform import` the preserved resources (Artifact Registry, VPC, subnet,
   peering, reserved IP) back into state. Idempotent — anything already in
   state is skipped; anything missing in GCP is left for apply to recreate.
2. If the Artifact Registry *was* destroyed last cycle (script bug or manual
   cleanup), the script falls back to a two-phase apply:
   a. `terraform apply -target` creates the registry + APIs only.
   b. `gh workflow run deploy-backend.yml --ref dev` triggers a build+push.
      The workflow's gcloud-run-deploy step will fail (Cloud Run doesn't
      exist yet), but the image push happens first and succeeds. The script
      polls the registry until the image lands.
3. `terraform apply -auto-approve` — creates everything else. Secrets get
   `PLACEHOLDER` values from the Terraform module (thanks to the
   `ignore_changes = [secret_data]` lifecycle, our restored values later
   won't cause drift). Cloud Run boots against the preserved `tardi/api:latest`
   image.
4. Read every file in `scripts/.dev-secrets-backup/` and push it as a new
   Secret Manager version via `gcloud secrets versions add`.
5. Bounce Cloud Run (`gcloud run services update --update-labels=wake-ts=…`)
   so the new revision picks up the real secret versions.
6. Poll `/readyz` up to 3 min. Exits non-zero on timeout.

Expected end-to-end time: 5–10 min.

## First-time / disaster recovery

If this is the first run ever, or the secrets backup was lost, you need to
seed `scripts/.dev-secrets-backup/` manually. One file per secret:

```
scripts/.dev-secrets-backup/
├── dev-admin-api-token.txt
├── dev-cloudflare-api-token.txt
├── dev-cloudflare-base-domain.txt
├── dev-cloudflare-zone-id.txt
├── dev-firebase-project-id.txt
├── dev-google-oauth-client-id.txt
├── dev-google-oauth-client-secret.txt
├── dev-hetzner-api-token.txt
├── dev-ssh-private-key.txt
├── dev-stripe-secret-key.txt
├── dev-stripe-webhook-secret.txt
├── dev-terminal-ticket-secret.txt
└── dev-token-encryption-key.txt
```

Each file holds the raw value — no trailing newline, no quotes. Same format
as what `gcloud secrets versions access latest` returns.

If the Artifact Registry was accidentally destroyed, `dev-bringup.sh` now
detects this and handles it automatically via a two-phase apply + workflow
dispatch (see the "Bring up" section above). No manual intervention required
beyond making sure `gh` is authenticated (`gh auth status`).

## What the scripts assume

- `gcloud` is authenticated and has access to `tardi-dev-488420`.
- `terraform` is available and `infra/environments/dev/` initialises cleanly.
- For `--dump-db` only: `cloud-sql-proxy` and `pg_dump` (from `libpq`) are
  installed. The dump path matches [backend/scripts/db-query.sh](../backend/scripts/db-query.sh).

## What's *not* automated

- CI workflow state. `deploy-backend.yml` and `deploy-frontend.yml` stay
  enabled during teardown by design — don't push to `dev` while torn down.
- Hetzner VPSes that users provisioned through dev. Dev is a test env;
  we don't expect real user VPSes. If any exist, they are not affected by
  teardown (they live on Hetzner, not GCP) but they will be orphaned since
  the backend DB that tracks them is gone.
- Prod. Prod is a separate Terraform root and GCP project. Untouched.
