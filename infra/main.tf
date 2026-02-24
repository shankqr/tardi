terraform {
  required_version = ">= 1.5"

  backend "gcs" {
    bucket = "tardi-terraform-state"
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

# Enable required GCP APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "secretmanager.googleapis.com",
    "artifactregistry.googleapis.com",
    "compute.googleapis.com",
    "servicenetworking.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "iam.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
  ])

  service            = each.value
  disable_on_destroy = false
}

# Instantiate dev environment
module "dev" {
  source = "./modules/backend-env"

  project_id   = var.project_id
  region       = var.region
  environment  = "dev"
  docker_image = "${var.region}-docker.pkg.dev/${var.project_id}/tardi/api:${var.docker_image_tag_dev}"
  frontend_url = var.frontend_url_dev
  db_tier      = var.db_tier_dev
  vpc_network  = google_compute_network.vpc.id

  depends_on = [
    google_project_service.apis,
    google_service_networking_connection.private_vpc,
  ]
}

# Instantiate prod environment
module "prod" {
  source = "./modules/backend-env"

  project_id   = var.project_id
  region       = var.region
  environment  = "prod"
  docker_image = "${var.region}-docker.pkg.dev/${var.project_id}/tardi/api:${var.docker_image_tag_prod}"
  frontend_url = var.frontend_url_prod
  db_tier      = var.db_tier_prod
  vpc_network  = google_compute_network.vpc.id

  depends_on = [
    google_project_service.apis,
    google_service_networking_connection.private_vpc,
  ]
}
