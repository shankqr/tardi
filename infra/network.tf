# VPC for Cloud SQL private networking
resource "google_compute_network" "vpc" {
  name                    = "tardi-vpc"
  auto_create_subnetworks = false

  depends_on = [google_project_service.apis]
}

resource "google_compute_subnetwork" "default" {
  name          = "tardi-subnet"
  ip_cidr_range = "10.0.0.0/24"
  region        = var.region
  network       = google_compute_network.vpc.id
}

# Private IP range for Cloud SQL
resource "google_compute_global_address" "private_ip" {
  name          = "tardi-sql-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.vpc.id
}

# Private services access for Cloud SQL
resource "google_service_networking_connection" "private_vpc" {
  network                 = google_compute_network.vpc.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip.name]
}
