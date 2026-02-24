output "api_url" {
  description = "Cloud Run service URL"
  value       = google_cloud_run_v2_service.api.uri
}

output "db_connection_name" {
  description = "Cloud SQL connection name"
  value       = google_sql_database_instance.db.connection_name
}

output "service_account_email" {
  description = "Cloud Run service account email"
  value       = google_service_account.api.email
}

output "artifact_registry_url" {
  description = "Docker image push target"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.tardi.repository_id}"
}
