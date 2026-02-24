# Database URL — constructed from Cloud SQL instance
resource "google_secret_manager_secret" "database_url" {
  secret_id = "${var.environment}-database-url"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "database_url" {
  secret      = google_secret_manager_secret.database_url.id
  secret_data = "postgres://tardi:${random_password.db_password.result}@/${google_sql_database.tardi.name}?host=/cloudsql/${google_sql_database_instance.db.connection_name}"
}

# Firebase project ID — set manually after creation
resource "google_secret_manager_secret" "firebase_project_id" {
  secret_id = "${var.environment}-firebase-project-id"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "firebase_project_id" {
  secret      = google_secret_manager_secret.firebase_project_id.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Stripe secret key — set manually after creation
resource "google_secret_manager_secret" "stripe_secret_key" {
  secret_id = "${var.environment}-stripe-secret-key"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "stripe_secret_key" {
  secret      = google_secret_manager_secret.stripe_secret_key.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Stripe webhook secret — set manually after creation
resource "google_secret_manager_secret" "stripe_webhook_secret" {
  secret_id = "${var.environment}-stripe-webhook-secret"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "stripe_webhook_secret" {
  secret      = google_secret_manager_secret.stripe_webhook_secret.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Hetzner API token — set manually after creation
resource "google_secret_manager_secret" "hetzner_api_token" {
  secret_id = "${var.environment}-hetzner-api-token"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "hetzner_api_token" {
  secret      = google_secret_manager_secret.hetzner_api_token.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}
