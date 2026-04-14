# Cut _Default log bucket retention in dev from the GCP default (30d) to 7d.
# Prod keeps the default — retaining recent prod logs is worth the few dollars.
# _Required bucket (audit logs) is unchanged.
resource "google_logging_project_bucket_config" "default" {
  count          = var.environment == "prod" ? 0 : 1
  project        = var.project_id
  location       = "global"
  bucket_id      = "_Default"
  retention_days = 7
}
