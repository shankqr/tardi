# BigQuery dataset that receives the daily billing export from the
# Cloud Billing account (017711-880A01-FCEF09).
#
# Terraform can create the dataset and enable the bigquery API, but
# *enabling* the billing export itself is not exposed by the google
# provider — it has to be turned on once via the console:
#
#   https://console.cloud.google.com/billing/017711-880A01-FCEF09/export
#     -> BigQuery export -> Edit settings
#     -> Project: tardi-prod-488420
#     -> Dataset: billing_export
#     -> Enable "Standard usage cost" (and "Pricing" if you want SKU prices)
#
# Once enabled, Google grants the export service agent the needed
# BigQuery permissions on the dataset automatically.

resource "google_project_service" "bigquery" {
  project            = var.project_id
  service            = "bigquery.googleapis.com"
  disable_on_destroy = false
}

resource "google_bigquery_dataset" "billing_export" {
  dataset_id    = "billing_export"
  project       = var.project_id
  location      = "US"
  description   = "Cloud Billing daily usage export (enable via Billing console)"
  friendly_name = "Billing Export"

  # Auto-expire individual rows after 400 days so the dataset doesn't
  # grow unbounded. Historical trend analysis beyond a year is rarely
  # useful at this stage.
  default_table_expiration_ms = 400 * 24 * 60 * 60 * 1000

  depends_on = [google_project_service.bigquery]
}
