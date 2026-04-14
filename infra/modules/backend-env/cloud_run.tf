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
        value = "info"
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
      env {
        name = "CLOUDFLARE_API_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.cloudflare_api_token.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "CLOUDFLARE_ZONE_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.cloudflare_zone_id.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "CLOUDFLARE_BASE_DOMAIN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.cloudflare_base_domain.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "SSH_PRIVATE_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.ssh_private_key.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "GOOGLE_OAUTH_CLIENT_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.google_oauth_client_id.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "GOOGLE_OAUTH_CLIENT_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.google_oauth_client_secret.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "ADMIN_API_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.admin_api_token.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "TOKEN_ENCRYPTION_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.token_encryption_key.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "TERMINAL_TICKET_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.terminal_ticket_secret.secret_id
            version = "latest"
          }
        }
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        cpu_idle = true
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
      min_instance_count = var.environment == "prod" ? 1 : 0
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

  # Image + deploy-time labels are owned by the deploy-backend workflow.
  # volume_mounts drift is reintroduced by the gcloud-based deploy even
  # though TF only defines the volume at template level; ignore it.
  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      template[0].containers[0].volume_mounts,
      template[0].labels,
      client,
      client_version,
    ]
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
