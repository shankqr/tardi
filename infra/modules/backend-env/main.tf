locals {
  api_url = var.api_url
}

# Enable required GCP APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "secretmanager.googleapis.com",
    "artifactregistry.googleapis.com",
    "compute.googleapis.com",
    "servicenetworking.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "iam.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
    "cloudscheduler.googleapis.com",
  ])

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "random_password" "db_password" {
  length  = 32
  special = false
}

resource "google_sql_database_instance" "db" {
  name             = "tardi-db-${var.environment}"
  database_version = "POSTGRES_16"
  region           = var.region
  project          = var.project_id

  settings {
    tier              = var.db_tier
    availability_type = "ZONAL"
    disk_size         = var.environment == "prod" ? 20 : 10
    disk_autoresize   = true

    ip_configuration {
      ipv4_enabled                                  = true
      private_network                               = google_compute_network.vpc.id
      enable_private_path_for_google_cloud_services = true
    }

    backup_configuration {
      enabled                        = var.environment == "prod"
      point_in_time_recovery_enabled = false
      start_time                     = "03:00"
      backup_retention_settings {
        retained_backups = var.environment == "prod" ? 3 : 1
      }
    }

    maintenance_window {
      day  = 7 # Sunday
      hour = 4 # 4 AM UTC
    }

    database_flags {
      name  = "log_min_duration_statement"
      value = var.environment == "prod" ? "1000" : "500"
    }
  }

  deletion_protection = var.environment == "prod"

  depends_on = [
    google_project_service.apis,
    google_service_networking_connection.private_vpc,
  ]
}

resource "google_sql_database" "tardi" {
  name     = "tardi"
  instance = google_sql_database_instance.db.name
  project  = var.project_id
}

resource "google_sql_user" "tardi" {
  name     = "tardi"
  instance = google_sql_database_instance.db.name
  password = random_password.db_password.result
  project  = var.project_id
}
