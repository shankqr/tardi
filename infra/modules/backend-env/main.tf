resource "random_password" "db_password" {
  length  = 32
  special = false
}

resource "google_sql_database_instance" "db" {
  name             = "tardi-db-${var.environment}"
  database_version = "POSTGRES_16"
  region           = var.region

  settings {
    tier              = var.db_tier
    availability_type = var.environment == "prod" ? "REGIONAL" : "ZONAL"
    disk_size         = var.environment == "prod" ? 20 : 10
    disk_autoresize   = true

    ip_configuration {
      ipv4_enabled                                  = false
      private_network                               = var.vpc_network
      enable_private_path_for_google_cloud_services = true
    }

    backup_configuration {
      enabled                        = var.environment == "prod"
      point_in_time_recovery_enabled = var.environment == "prod"
      start_time                     = "03:00"
      backup_retention_settings {
        retained_backups = var.environment == "prod" ? 7 : 1
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
}

resource "google_sql_database" "tardi" {
  name     = "tardi"
  instance = google_sql_database_instance.db.name
}

resource "google_sql_user" "tardi" {
  name     = "tardi"
  instance = google_sql_database_instance.db.name
  password = random_password.db_password.result
}
