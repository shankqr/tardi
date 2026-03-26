# Notification channel — update email after creation
resource "google_monitoring_notification_channel" "email" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "Tardi Alerts Email"
  type         = "email"

  labels = {
    email_address = var.alert_email
  }
}

# Cloud Run error rate > 5%
resource "google_monitoring_alert_policy" "cloud_run_errors" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "Cloud Run Error Rate > 5%"
  combiner     = "OR"

  conditions {
    display_name = "Cloud Run 5xx error rate"

    condition_threshold {
      filter          = "resource.type = \"cloud_run_revision\" AND metric.type = \"run.googleapis.com/request_count\" AND metric.labels.response_code_class = \"5xx\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 5

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
        group_by_fields      = ["resource.label.service_name"]
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].name]

  alert_strategy {
    auto_close = "1800s"
  }
}

# Cloud Run p95 latency > 2s
resource "google_monitoring_alert_policy" "cloud_run_latency" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "Cloud Run p95 Latency > 2s"
  combiner     = "OR"

  conditions {
    display_name = "Cloud Run request latency p95"

    condition_threshold {
      filter          = "resource.type = \"cloud_run_revision\" AND metric.type = \"run.googleapis.com/request_latencies\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 2000

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_PERCENTILE_95"
        cross_series_reducer = "REDUCE_MAX"
        group_by_fields      = ["resource.label.service_name"]
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].name]

  alert_strategy {
    auto_close = "1800s"
  }
}

# Cloud SQL CPU > 80%
resource "google_monitoring_alert_policy" "cloud_sql_cpu" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "Cloud SQL CPU > 80%"
  combiner     = "OR"

  conditions {
    display_name = "Cloud SQL CPU utilization"

    condition_threshold {
      filter          = "resource.type = \"cloudsql_database\" AND metric.type = \"cloudsql.googleapis.com/database/cpu/utilization\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 0.8

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_MEAN"
        cross_series_reducer = "REDUCE_MAX"
        group_by_fields      = ["resource.label.database_id"]
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].name]

  alert_strategy {
    auto_close = "1800s"
  }
}

# --- Uptime Checks (synthetic monitoring) ---

# API health check — pings /healthz every 5 minutes from multiple regions
resource "google_monitoring_uptime_check_config" "api_health" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "API Health Check (${var.environment})"
  timeout      = "10s"
  period       = "300s"

  http_check {
    path         = "/healthz"
    port         = 443
    use_ssl      = true
    validate_ssl = true
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = replace(var.api_url, "https://", "")
    }
  }
}

# Alert when API uptime check fails
resource "google_monitoring_alert_policy" "api_uptime" {
  count        = var.enable_monitoring ? 1 : 0
  project      = var.project_id
  display_name = "API Uptime Check Failed (${var.environment})"
  combiner     = "OR"

  conditions {
    display_name = "API uptime check failing"

    condition_threshold {
      filter          = "resource.type = \"uptime_url\" AND metric.type = \"monitoring.googleapis.com/uptime_check/check_passed\" AND metric.labels.check_id = \"${google_monitoring_uptime_check_config.api_health[0].uptime_check_id}\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 1

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].name]

  alert_strategy {
    auto_close = "1800s"
  }
}

# Frontend availability check — pings app.tardi.ai every 5 minutes (prod only)
resource "google_monitoring_uptime_check_config" "frontend_health" {
  count        = var.enable_monitoring && var.environment == "prod" ? 1 : 0
  project      = var.project_id
  display_name = "Frontend Availability (${var.environment})"
  timeout      = "10s"
  period       = "300s"

  http_check {
    path         = "/"
    port         = 443
    use_ssl      = true
    validate_ssl = true
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = replace(var.frontend_url, "https://", "")
    }
  }
}

# Alert when frontend uptime check fails
resource "google_monitoring_alert_policy" "frontend_uptime" {
  count        = var.enable_monitoring && var.environment == "prod" ? 1 : 0
  project      = var.project_id
  display_name = "Frontend Uptime Check Failed"
  combiner     = "OR"

  conditions {
    display_name = "Frontend uptime check failing"

    condition_threshold {
      filter          = "resource.type = \"uptime_url\" AND metric.type = \"monitoring.googleapis.com/uptime_check/check_passed\" AND metric.labels.check_id = \"${google_monitoring_uptime_check_config.frontend_health[0].uptime_check_id}\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 1

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].name]

  alert_strategy {
    auto_close = "1800s"
  }
}
