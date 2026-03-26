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

# Cloudflare API token — for DNS record management (Let's Encrypt domains)
resource "google_secret_manager_secret" "cloudflare_api_token" {
  secret_id = "${var.environment}-cloudflare-api-token"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "cloudflare_api_token" {
  secret      = google_secret_manager_secret.cloudflare_api_token.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Cloudflare zone ID — for tardi.ai DNS zone
resource "google_secret_manager_secret" "cloudflare_zone_id" {
  secret_id = "${var.environment}-cloudflare-zone-id"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "cloudflare_zone_id" {
  secret      = google_secret_manager_secret.cloudflare_zone_id.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# SSH private key — Ed25519 key for backend SSH access to VPSes (base64-encoded PEM)
resource "google_secret_manager_secret" "ssh_private_key" {
  secret_id = "${var.environment}-ssh-private-key"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "ssh_private_key" {
  secret      = google_secret_manager_secret.ssh_private_key.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Cloudflare base domain — subdomain pattern for agent instances
resource "google_secret_manager_secret" "cloudflare_base_domain" {
  secret_id = "${var.environment}-cloudflare-base-domain"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "cloudflare_base_domain" {
  secret      = google_secret_manager_secret.cloudflare_base_domain.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Google OAuth client ID — for delegated Google account access
resource "google_secret_manager_secret" "google_oauth_client_id" {
  secret_id = "${var.environment}-google-oauth-client-id"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "google_oauth_client_id" {
  secret      = google_secret_manager_secret.google_oauth_client_id.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Google OAuth client secret — for delegated Google account access
resource "google_secret_manager_secret" "google_oauth_client_secret" {
  secret_id = "${var.environment}-google-oauth-client-secret"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "google_oauth_client_secret" {
  secret      = google_secret_manager_secret.google_oauth_client_secret.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Admin API token — for internal admin endpoint authentication
resource "google_secret_manager_secret" "admin_api_token" {
  secret_id = "${var.environment}-admin-api-token"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "admin_api_token" {
  secret      = google_secret_manager_secret.admin_api_token.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}

# Token encryption key — AES-256-GCM key for encrypting OAuth tokens at rest
resource "google_secret_manager_secret" "token_encryption_key" {
  secret_id = "${var.environment}-token-encryption-key"
  project   = var.project_id

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "token_encryption_key" {
  secret      = google_secret_manager_secret.token_encryption_key.id
  secret_data = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [secret_data]
  }
}
