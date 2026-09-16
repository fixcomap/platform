# Lo mínimo para que las capas superiores puedan arrancar: leer el proyecto,
# habilitar más APIs, crear SAs y federar identidades. El resto se habilita en L1.
# disable_on_destroy = false: destruir esta capa no debe tumbar servicios en uso.
resource "google_project_service" "bootstrap" {
  for_each = toset([
    "serviceusage.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "sts.googleapis.com",
    "storage.googleapis.com",
  ])

  service            = each.value
  disable_on_destroy = false
}
