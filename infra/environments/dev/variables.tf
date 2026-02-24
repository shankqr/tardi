variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for all resources"
  type        = string
  default     = "us-central1"
}

variable "db_tier" {
  description = "Cloud SQL machine tier"
  type        = string
  default     = "db-f1-micro"
}

variable "frontend_url" {
  description = "Cloudflare Pages frontend URL"
  type        = string
  default     = "https://dev.tardi.pages.dev"
}

variable "docker_image_tag" {
  description = "Docker image tag for Cloud Run"
  type        = string
  default     = "latest"
}
