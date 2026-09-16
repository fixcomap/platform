# APIs de los servicios que gestionan L1 y L2. Las de IAM/STS/Storage ya están en L0.
resource "google_project_service" "platform" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "monitoring.googleapis.com",
  ])

  service            = each.value
  disable_on_destroy = false
}
