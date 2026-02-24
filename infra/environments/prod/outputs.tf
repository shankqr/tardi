output "api_url" {
  description = "Cloud Run prod service URL"
  value       = module.env.api_url
}

output "db_connection_name" {
  description = "Cloud SQL prod connection name"
  value       = module.env.db_connection_name
}

output "artifact_registry_url" {
  description = "Docker image push target"
  value       = module.env.artifact_registry_url
}
