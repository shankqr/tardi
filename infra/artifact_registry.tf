resource "google_artifact_registry_repository" "tardi" {
  repository_id = "tardi"
  location      = var.region
  format        = "DOCKER"
  description   = "Docker images for Tardi API"

  depends_on = [google_project_service.apis]
}
