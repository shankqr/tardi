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
    id     = "delete-old-tagged-after-30d"
    action = "DELETE"
    condition {
      tag_state  = "TAGGED"
      older_than = "2592000s"
    }
  }

  cleanup_policies {
    id     = "keep-5-most-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 5
    }
  }

  depends_on = [google_project_service.apis]
}
