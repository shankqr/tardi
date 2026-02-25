terraform {
  required_version = ">= 1.5"

  backend "gcs" {
    bucket = "tardi-prod-488420-terraform-state"
    prefix = "terraform/state"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

module "env" {
  source = "../../modules/backend-env"

  project_id        = var.project_id
  region            = var.region
  environment       = "prod"
  docker_image      = "${var.region}-docker.pkg.dev/${var.project_id}/tardi/api:${var.docker_image_tag}"
  frontend_url      = var.frontend_url
  db_tier           = var.db_tier
  enable_monitoring = true
  alert_email       = "alerts@tardi.dev"
}
