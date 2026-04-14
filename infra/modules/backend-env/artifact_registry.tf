resource "google_artifact_registry_repository" "tardi" {
  repository_id = "tardi"
  project       = var.project_id
  location      = var.region
  format        = "DOCKER"
  description   = "Docker images for Tardi API"

  cleanup_policies {
    id     = "delete-untagged-after-7d"
    action = "DELETE"
    condition {
      tag_state  = "UNTAGGED"
      older_than = "604800s"
    }
  }

  cleanup_policies {
    id     = "keep-10-most-recent-tagged"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }

  depends_on = [google_project_service.apis]
}
