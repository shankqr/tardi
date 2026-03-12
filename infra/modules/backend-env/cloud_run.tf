resource "google_cloud_run_v2_service" "api" {
  name     = "tardi-api-${var.environment}"
  location = var.region
  project  = var.project_id

  template {
    service_account = google_service_account.api.email

    # Direct VPC egress so Cloud SQL Auth Proxy can reach private IP
    vpc_access {
      network_interfaces {
        network    = google_compute_network.vpc.id
        subnetwork = google_compute_subnetwork.default.id
      }
      egress = "PRIVATE_RANGES_ONLY"
    }

    containers {
      image = var.docker_image

      ports {
        container_port = 8080
      }

      # Plain env vars
      env {
        name  = "ENVIRONMENT"
        value = var.environment
      }
      env {
        name  = "ALLOWED_ORIGINS"
        value = var.frontend_url
      }
      env {
        name  = "LOG_LEVEL"
        value = var.environment == "prod" ? "info" : "debug"
      }
      env {
        name  = "API_URL"
        value = local.api_url
      }

      # Secrets as env vars
      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.database_url.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "FIREBASE_PROJECT_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.firebase_project_id.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "STRIPE_SECRET_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.stripe_secret_key.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "STRIPE_WEBHOOK_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.stripe_webhook_secret.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "HETZNER_API_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.hetzner_api_token.secret_id
            version = "latest"
          }
        }
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      startup_probe {
        http_get {
          path = "/readyz"
        }
        initial_delay_seconds = 10
        period_seconds        = 5
        failure_threshold     = 6
      }

      liveness_probe {
        http_get {
          path = "/readyz"
        }
        period_seconds = 30
      }
    }

    scaling {
      min_instance_count = 1
      max_instance_count = var.environment == "prod" ? 10 : 2
    }

    # Cloud SQL Auth Proxy connection
    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.db.connection_name]
      }
    }
  }
}

# Allow unauthenticated access (API handles auth via Firebase JWT)
resource "google_cloud_run_v2_service_iam_member" "public" {
  name     = google_cloud_run_v2_service.api.name
  location = var.region
  project  = var.project_id
  role     = "roles/run.invoker"
  member   = "allUsers"
}
