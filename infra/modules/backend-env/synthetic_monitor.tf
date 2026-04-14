# Synthetic Monitor: Cloud Run Job invoked every 10 minutes by Cloud Scheduler.
# Replaces the health-checks GitHub Actions workflow (which was consuming
# ~8,600 Actions minutes/month). Prod-only.
#
# Bootstrap after first apply:
#   printf '<FINE_GRAINED_PAT>' | gcloud secrets versions add prod-synthetic-monitor-gh-token --data-file=- --project=tardi-prod-488420
#
# The PAT needs `issues: read+write` on shankqr/tardi only.

locals {
  synthetic_monitor_enabled = var.environment == "prod"
}

resource "google_service_account" "synthetic_monitor" {
  count        = local.synthetic_monitor_enabled ? 1 : 0
  account_id   = "tardi-synthetic-monitor"
  display_name = "Tardi Synthetic Monitor"
}

resource "google_secret_manager_secret" "synthetic_monitor_gh_token" {
  count     = local.synthetic_monitor_enabled ? 1 : 0
  secret_id = "${var.environment}-synthetic-monitor-gh-token"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "synthetic_monitor_gh_token" {
  count       = local.synthetic_monitor_enabled ? 1 : 0
  secret      = google_secret_manager_secret.synthetic_monitor_gh_token[0].id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

resource "google_secret_manager_secret_iam_member" "synthetic_monitor_gh_token_accessor" {
  count     = local.synthetic_monitor_enabled ? 1 : 0
  project   = var.project_id
  secret_id = google_secret_manager_secret.synthetic_monitor_gh_token[0].secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.synthetic_monitor[0].email}"
}

resource "google_project_iam_member" "synthetic_monitor_log_writer" {
  count   = local.synthetic_monitor_enabled ? 1 : 0
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.synthetic_monitor[0].email}"
}

resource "google_cloud_run_v2_job" "synthetic_monitor" {
  count    = local.synthetic_monitor_enabled ? 1 : 0
  name     = "tardi-synthetic-monitor"
  location = var.region
  project  = var.project_id

  template {
    template {
      service_account = google_service_account.synthetic_monitor[0].email
      max_retries     = 0
      timeout         = "120s"

      containers {
        image   = var.docker_image
        command = ["/synthetic-monitor"]

        env {
          name  = "API_URL"
          value = local.api_url
        }
        env {
          name  = "FRONTEND_URL"
          value = var.frontend_url
        }
        env {
          name  = "GITHUB_REPO"
          value = "shankqr/tardi"
        }
        env {
          name = "GITHUB_TOKEN"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.synthetic_monitor_gh_token[0].secret_id
              version = "latest"
            }
          }
        }

        resources {
          limits = {
            cpu    = "1"
            memory = "256Mi"
          }
        }
      }
    }
  }

  # Ignore image tag churn — image is updated by the deploy workflow, not TF.
  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
    ]
  }
}

resource "google_service_account" "synthetic_monitor_scheduler" {
  count        = local.synthetic_monitor_enabled ? 1 : 0
  account_id   = "tardi-synth-scheduler"
  display_name = "Tardi Synthetic Monitor Scheduler Invoker"
}

resource "google_cloud_run_v2_job_iam_member" "scheduler_invoker" {
  count    = local.synthetic_monitor_enabled ? 1 : 0
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.synthetic_monitor[0].name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.synthetic_monitor_scheduler[0].email}"
}

resource "google_cloud_scheduler_job" "synthetic_monitor" {
  count            = local.synthetic_monitor_enabled ? 1 : 0
  name             = "tardi-synthetic-monitor"
  description      = "Invoke synthetic monitor every 10 minutes"
  schedule         = "*/10 * * * *"
  time_zone        = "Etc/UTC"
  region           = var.region
  project          = var.project_id
  attempt_deadline = "180s"

  retry_config {
    retry_count = 0
  }

  http_target {
    http_method = "POST"
    uri         = "https://${var.region}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${var.project_id}/jobs/${google_cloud_run_v2_job.synthetic_monitor[0].name}:run"

    oauth_token {
      service_account_email = google_service_account.synthetic_monitor_scheduler[0].email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }
}
