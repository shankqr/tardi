# Synthetic Monitoring

Health checks against prod run on **Cloud Scheduler → Cloud Run Job** (in
`tardi-prod-2026`), not GitHub Actions. The only thing left in
[.github/workflows/synthetic-monitoring.yml](../.github/workflows/synthetic-monitoring.yml)
is the hourly Playwright "deep check" smoke test.

## Why

The previous `*/10` Actions cron for health checks + Playwright was
burning ~24,000 Actions minutes/month against a 2,000-minute free tier,
causing the whole GitHub Actions tenant to fail on spending-limit
overage. Moving the lightweight checks to Cloud Run Jobs saves ~8,600
min/month; cutting Playwright from 10-min to hourly saves another
~13,000 min/month.

## What it checks

Source: [backend/cmd/synthetic-monitor/main.go](../backend/cmd/synthetic-monitor/main.go)

- `GET https://tardi-api-prod-…/readyz`
- `GET https://tardi-api-prod-…/api/models`
- `GET https://app.tardi.ai/` (body must contain `tardi`)
- `GET https://app.tardi.ai/login`
- SSL certificate expiry for `app.tardi.ai` (warns <14 days)

On any failure it opens (or comments on) a GitHub issue labeled `outage`
in `shankqr/tardi`. On recovery it comments and closes the issue.

## Infrastructure

Defined in [infra/modules/backend-env/synthetic_monitor.tf](../infra/modules/backend-env/synthetic_monitor.tf)
with `count = var.environment == "prod" ? 1 : 0` (prod-only).

| Resource | Name |
|---|---|
| Cloud Run Job | `tardi-synthetic-monitor` |
| Cloud Scheduler | `tardi-synthetic-monitor` (`*/10 * * * *`) |
| Service account (job) | `tardi-synthetic-monitor@…` |
| Service account (scheduler) | `tardi-synth-scheduler@…` |
| Secret | `prod-synthetic-monitor-gh-token` |

The job runs the `synthetic-monitor` binary baked into the existing
backend image (`us-central1-docker.pkg.dev/tardi-prod-2026/tardi/api:latest`),
so it rolls forward automatically on every backend deploy.

## First-time bootstrap

1. **Apply Terraform** in `infra/environments/prod/`:
   ```bash
   cd infra/environments/prod
   terraform apply
   ```
2. **Create a fine-grained GitHub PAT** at
   https://github.com/settings/personal-access-tokens/new
   - Repository access: Only `shankqr/tardi`
   - Permissions: Repository → **Issues: Read and write**
   - Expiration: 1 year (set a calendar reminder to rotate)
3. **Populate the secret**:
   ```bash
   printf '<PAT>' | gcloud secrets versions add prod-synthetic-monitor-gh-token \
     --data-file=- --project=tardi-prod-2026
   ```
4. **Trigger a test run**:
   ```bash
   gcloud scheduler jobs run tardi-synthetic-monitor \
     --location=us-central1 --project=tardi-prod-2026
   gcloud run jobs executions list --job=tardi-synthetic-monitor \
     --region=us-central1 --project=tardi-prod-2026 --limit=1
   ```
5. **Tail logs**:
   ```bash
   gcloud logging read \
     'resource.type=cloud_run_job AND resource.labels.job_name=tardi-synthetic-monitor' \
     --limit=20 --project=tardi-prod-2026 --format='value(jsonPayload.msg,jsonPayload.check,jsonPayload.ok)'
   ```

## Operational notes

- **Changing cadence**: edit `schedule` in `synthetic_monitor.tf` and `terraform apply`.
- **Pausing**: `gcloud scheduler jobs pause tardi-synthetic-monitor --location=us-central1 --project=tardi-prod-2026`
- **Rolling the PAT**: add a new secret version (step 3 above). The job
  reads `latest` on every execution, so no redeploy needed.
- **Alerting destination**: currently GitHub issues only. To add Sentry
  or Slack, extend `reportFailure()` in
  [backend/cmd/synthetic-monitor/main.go](../backend/cmd/synthetic-monitor/main.go).
