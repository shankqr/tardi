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
