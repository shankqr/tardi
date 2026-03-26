# CI/CD

GitHub Actions with two branches as source of truth:

- **`dev` branch** → development environment
- **`main` branch** → production environment

## Workflows (`.github/workflows/`)

| File                  | Trigger                              | Purpose                                                                                |
| --------------------- | ------------------------------------ | -------------------------------------------------------------------------------------- |
| `ci-gate.yml`         | PR to `dev`/`main`                   | Path-based change detection, calls reusable CI workflows, single required status check |
| `ci-frontend.yml`     | Reusable + PR                        | `npm run check` + `npm run build`                                                      |
| `ci-backend.yml`      | Reusable + PR                        | `golangci-lint` + `go test` + `go build` (3 parallel jobs)                             |
| `ci-infra.yml`        | Reusable + PR                        | `terraform fmt -check` + `validate` + `plan` (posts plan as PR comment)                |
| `deploy-frontend.yml` | Push to `dev`/`main` (frontend/\*\*) | Build + Wrangler deploy to Cloudflare Pages                                            |
| `deploy-backend.yml`  | Push to `dev`/`main` (backend/\*\*)  | Docker build → Artifact Registry → Cloud Run deploy                                    |
| `deploy-infra.yml`    | Push to `main` only (infra/\*\*)     | `terraform apply` (dev then prod, separate roots)                                      |

## Branch-to-Environment Mapping

| Branch | Frontend                | Backend        | Image Tags               |
| ------ | ----------------------- | -------------- | ------------------------ |
| `dev`  | dev.tardi-467.pages.dev | tardi-api-dev  | `dev-{sha7}` + `latest`  |
| `main` | app.tardi.ai            | tardi-api-prod | `prod-{sha7}` + `stable` |

## Key Design Decisions

- **CI Gate pattern**: `dorny/paths-filter` detects changes, conditionally runs component CI workflows. Single `gate` job is the only required status check (solves path-filter + required-check incompatibility)
- **Infra applies from `main` only**: Separate Terraform roots per project, applied sequentially (dev then prod)
- **GCP auth**: Workload Identity Federation (no long-lived service account keys)
- **Concurrency**: Per-branch groups with `cancel-in-progress: false` (running deploys finish)
- **Runtime vars** (`COMING_SOON`, `API_URL`): Set in `wrangler.toml` per environment, can be overridden in Cloudflare dashboard

## GitHub Secrets Required

- `GCP_PROJECT_ID`, `GCP_REGION`, `GCP_WORKLOAD_IDENTITY_PROVIDER`, `GCP_SERVICE_ACCOUNT`
- `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`
- `VITE_FIREBASE_API_KEY`, `VITE_FIREBASE_AUTH_DOMAIN`, `VITE_FIREBASE_PROJECT_ID`, `VITE_FIREBASE_STORAGE_BUCKET`, `VITE_FIREBASE_MESSAGING_SENDER_ID`, `VITE_FIREBASE_APP_ID`
