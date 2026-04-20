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
2. `terraform state rm module.env.google_artifact_registry_repository.tardi` —
   the Artifact Registry is left alive in GCP so images survive the destroy.
   Without this, bring-up has no image for Cloud Run to pull.
3. `terraform destroy -auto-approve` in [infra/environments/dev/](../infra/environments/dev/).

What gets destroyed: Cloud SQL (and its data), Cloud Run, VPC + subnet + private
service peering, IAM service account, logging bucket config, all 14 Secret
Manager entries, API enablement.

What survives: Artifact Registry `tardi` (images), Terraform state bucket
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

1. `terraform import` the Artifact Registry back into state (idempotent).
2. `terraform apply -auto-approve` — recreates everything. Secrets get
   `PLACEHOLDER` values from the Terraform module (thanks to the
   `ignore_changes = [secret_data]` lifecycle, our restored values later
   won't cause drift). Cloud Run boots against the preserved `tardi/api:latest`
   image.
3. Read every file in `scripts/.dev-secrets-backup/` and push it as a new
   Secret Manager version via `gcloud secrets versions add`.
4. Bounce Cloud Run (`gcloud run services update --update-labels=wake-ts=…`)
   so the new revision picks up the real secret versions.
5. Poll `/readyz` up to 3 min. Exits non-zero on timeout.

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

If the Artifact Registry was accidentally destroyed:

```bash
# Push a fresh image first (the deploy-backend workflow will fail at its
# gcloud-run-deploy step because the service doesn't exist yet, but the
# build+push step will have already landed an image in the registry):
gh workflow run deploy-backend.yml --ref dev
gh run watch   # wait for image push to finish

# Then re-run bring-up
./scripts/dev-bringup.sh
```

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
