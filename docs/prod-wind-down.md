# Production GCP wind-down

Production was intentionally torn down on 2026-07-28 while the Tardi project
is paused. This was a destructive teardown, not a suspend-in-place operation.

## Current state

- `tardi-prod-2026` is unlinked from billing (`billingEnabled: false`).
- Cloud Run, Cloud Run Jobs, Cloud SQL and its automated backups, Cloud
  Scheduler, Artifact Registry, Secret Manager, monitoring, and the BigQuery
  billing dataset were deleted.
- `gs://tardi-prod-2026-terraform-state` and every version of its Terraform
  state were deleted.
- No database export or secret backup was retained as part of the teardown.
- The Terraform configuration remains in the repository so the environment can
  be rebuilt from scratch.
- Cloud Run Direct VPC reservations can remain visible for 1–2 hours after
  service deletion. They and the empty VPC resources are non-billable and are
  released asynchronously by Google.
- `tardi-dev-488420` and legacy `tardi-prod-488420` are also billing-disabled.
  The legacy project remains only to preserve the zero-cost Firebase Auth
  tenant.

## Bring production back

1. Relink billing to `tardi-prod-2026` using the `tardi-prod-2026` gcloud
   configuration.
2. Recreate `gs://tardi-prod-2026-terraform-state` in `US`, enable uniform
   bucket-level access, and enable object versioning.
3. Run `terraform init -reconfigure` and `terraform apply` from
   `infra/environments/prod`.
4. Replace every Terraform-created `PLACEHOLDER` secret with newly issued
   production credentials. The deleted secret values cannot be recovered from
   GCP or Terraform state.
5. Build and push the backend image, deploy Cloud Run, and run all database
   migrations.
6. Deploy the frontend and refresh any Cloudflare proxy target that contains a
   generated Cloud Run hostname.
7. Verify `/readyz`, Stripe webhooks, provisioning, Google OAuth, Firebase Auth,
   the synthetic monitor, and billing alerts before reopening the product.

Do not run the ordinary production deploy commands until steps 1–4 are
complete; the Artifact Registry repository and deployment secrets do not exist.
