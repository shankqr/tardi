variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for all resources"
  type        = string
  default     = "us-central1"
}

variable "db_tier_dev" {
  description = "Cloud SQL machine tier for dev"
  type        = string
  default     = "db-f1-micro"
}

variable "db_tier_prod" {
  description = "Cloud SQL machine tier for prod"
  type        = string
  default     = "db-custom-1-3840"
}

variable "frontend_url_dev" {
  description = "Cloudflare Pages preview URL (dev frontend)"
  type        = string
  default     = "https://dev.tardi.pages.dev"
}

variable "frontend_url_prod" {
  description = "Cloudflare Pages production URL"
  type        = string
  default     = "https://tardi.pages.dev"
}

variable "docker_image_tag_dev" {
  description = "Docker image tag for dev Cloud Run"
  type        = string
  default     = "latest"
}

variable "docker_image_tag_prod" {
  description = "Docker image tag for prod Cloud Run"
  type        = string
  default     = "latest"
}
