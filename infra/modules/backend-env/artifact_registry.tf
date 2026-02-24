resource "google_artifact_registry_repository" "tardi" {
  repository_id = "tardi"
  project       = var.project_id
  location      = var.region
  format        = "DOCKER"
  description   = "Docker images for Tardi API"

  depends_on = [google_project_service.apis]
}
