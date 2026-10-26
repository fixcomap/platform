# Una identidad de despliegue por webapp (<service>-deployer): el pipeline de
# un repo solo puede tocar sus propios servicios, su imagen, su estado y sus
# secretos de migración, no los de las demás webapps.
#
# Cloud Run no admite IAM conditions por nombre de recurso, así que run.developer
# se da sobre cada servicio, y eso exige que exista. Un servicio existe cuando su
# entorno es public (alta en dos releases): hasta entonces lo crea gh-deployer,
# solo desde la rama de ese entorno.
locals {
  # Servicio => webapp, entorno y si ya despliega con su propia identidad.
  webapp_deploy = {
    for s, e in local.webapp_services : s => {
      webapp      = e.webapp
      environment = e.environment
      own         = e.public
    }
  }

  # "<webapp>:<rama>" de webapp_deploy_refs => servicio de ese entorno.
  webapp_ref_service = merge([
    for k, w in local.webapps : { "${k}:main" = w.service, "${k}:develop" = "${w.service}-dev" }
  ]...)

  # Webapps con algún servicio que aún crea gh-deployer.
  webapps_pendientes = distinct([for s, d in local.webapp_deploy : d.webapp if !d.own])
}

resource "google_service_account" "webapp_deployer" {
  for_each = local.webapps

  account_id   = "${each.value.service}-deployer"
  display_name = "Pipeline de ${each.key}: despliega solo sus servicios (catalog/${each.key}.yaml)"
}

# Desde main y develop de su repo; nada más.
resource "google_service_account_iam_member" "webapp_deployer_wif" {
  for_each = local.webapp_deploy_refs

  service_account_id = google_service_account.webapp_deployer[split(":", each.key)[0]].name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository_ref/${each.value}"
}

# Lectura de Cloud Run en el proyecto: el apply espera las operaciones, que no
# cuelgan de ningún servicio. No incluye secretos (solo sus referencias).
resource "google_project_iam_member" "webapp_deployer_run_viewer" {
  for_each = local.webapps

  project = var.project_id
  role    = "roles/run.viewer"
  member  = google_service_account.webapp_deployer[each.key].member
}

resource "google_cloud_run_v2_service_iam_member" "webapp_deployer_run" {
  for_each = { for s, d in local.webapp_deploy : s => d if d.own }

  project  = var.project_id
  location = var.region
  name     = each.key
  role     = "roles/run.developer"
  member   = google_service_account.webapp_deployer[each.value.webapp].member
}

resource "google_service_account_iam_member" "webapp_deployer_actas" {
  for_each = local.webapps

  service_account_id = google_service_account.webapp_runtime[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.webapp_deployer[each.key].member
}

# Artifact Registry tampoco admite conditions por imagen: escritura en el
# repositorio compartido. El deploy usa el digest que construyó su propio job,
# así que una imagen ajena con el mismo tag no llega a desplegarse.
resource "google_artifact_registry_repository_iam_member" "webapp_deployer_writer" {
  for_each = local.webapps

  location   = google_artifact_registry_repository.docker.location
  repository = google_artifact_registry_repository.docker.name
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.webapp_deployer[each.key].member
}

resource "google_secret_manager_secret_iam_member" "webapp_deployer_migrate" {
  for_each = local.webapp_migrate_secrets

  secret_id = google_secret_manager_secret.webapp_migrate[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.webapp_deployer[each.value.webapp].member
}

# Estado de L2 en un bucket por webapp: las condiciones por prefijo no cubren
# storage.objects.list, que el backend gcs necesita.
resource "google_storage_bucket" "webapp_state" {
  # checkov:skip=CKV_GCP_62: mismo motivo que los buckets de estado de L0 (access logs con coste; Admin Activity ya registra cambios)
  for_each = local.webapps

  name                        = "${var.project_id}-tfstate-${each.key}"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      num_newer_versions = 10
      with_state         = "ARCHIVED"
    }
    action {
      type = "Delete"
    }
  }
}

resource "google_storage_bucket_iam_member" "webapp_deployer_state" {
  for_each = local.webapps

  bucket = google_storage_bucket.webapp_state[each.key].name
  role   = "roles/storage.objectUser"
  member = google_service_account.webapp_deployer[each.key].member
}

# gh-deployer crea los servicios que aún no existen: necesita el estado de esa webapp.
resource "google_storage_bucket_iam_member" "gh_deployer_webapp_state" {
  for_each = toset(local.webapps_pendientes)

  bucket = google_storage_bucket.webapp_state[each.key].name
  role   = "roles/storage.objectUser"
  member = google_service_account.gh_deployer.member
}
