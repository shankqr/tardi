variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
}

variable "environment" {
  description = "Environment name: dev or prod"
  type        = string

  validation {
    condition     = contains(["dev", "prod"], var.environment)
    error_message = "Environment must be 'dev' or 'prod'."
  }
}

variable "docker_image" {
  description = "Full Docker image path with tag"
  type        = string
}

variable "frontend_url" {
  description = "Cloudflare Pages frontend URL (for CORS)"
  type        = string
}

variable "db_tier" {
  description = "Cloud SQL machine tier"
  type        = string
}

variable "vpc_network" {
  description = "VPC network ID for Cloud SQL private IP"
  type        = string
}
