output "dev_api_url" {
  description = "Cloud Run dev service URL — set as API_URL in Cloudflare Pages preview"
  value       = module.dev.api_url
}

output "prod_api_url" {
  description = "Cloud Run prod service URL — set as API_URL in Cloudflare Pages production"
  value       = module.prod.api_url
}

output "dev_db_connection" {
  description = "Cloud SQL dev connection name"
  value       = module.dev.db_connection_name
}

output "prod_db_connection" {
  description = "Cloud SQL prod connection name"
  value       = module.prod.db_connection_name
}

output "artifact_registry_url" {
  description = "Docker image push target"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.tardi.repository_id}"
}
