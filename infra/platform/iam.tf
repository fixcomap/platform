locals {
  wif_pool = "projects/${var.project_number}/locations/global/workloadIdentityPools/${var.wif_pool_id}"
  repo     = "${var.github_org}/${var.github_repo}"
}

# ---------- gh-deployer ----------

# Cloud Run: developer y no admin. La diferencia es setIamPolicy: gh-deployer no
# puede dar permisos de invocación a nadie. El acceso público se resuelve con
# invoker_iam_disabled en el propio servicio (L2).
resource "google_project_iam_member" "gh_deployer_run" {
  project = var.project_id
  role    = "roles/run.developer"
  member  = google_service_account.gh_deployer.member
}

# actAs sobre app-runtime, y solo sobre ella: es lo que Cloud Run exige para
# desplegar un servicio con esa identidad.
resource "google_service_account_iam_member" "gh_deployer_actas_runtime" {
  service_account_id = google_service_account.app_runtime.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gh_deployer.member
}

# Push de imágenes y firmas cosign, limitado al repositorio.
resource "google_artifact_registry_repository_iam_member" "gh_deployer_writer" {
  location   = google_artifact_registry_repository.docker.location
  repository = google_artifact_registry_repository.docker.name
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.gh_deployer.member
}

# Estado de L2 y nada más: bucket propio, sin condiciones. Las condiciones IAM
# por prefijo no autorizan storage.objects.list, que el backend gcs necesita en
# init, así que el aislamiento se hace por bucket y no por prefijo.
resource "google_storage_bucket_iam_member" "gh_deployer_state" {
  bucket = var.state_bucket_app
  role   = "roles/storage.objectUser"
  member = google_service_account.gh_deployer.member
}

# Solo desde main. En PR no hay token para gh-deployer, así que un PR no puede
# desplegar ni publicar imágenes.
resource "google_service_account_iam_member" "gh_deployer_wif" {
  service_account_id = google_service_account.gh_deployer.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository_ref/${local.repo}:refs/heads/main"
}

# ---------- app-runtime ----------

# Lee el secreto de configuración. secretAccessor sobre el secreto concreto,
# no sobre el proyecto.
resource "google_secret_manager_secret_iam_member" "app_runtime_config" {
  secret_id = google_secret_manager_secret.app_config.id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.app_runtime.member
}

# ---------- tf-plan ----------

# viewer a nivel de proyecto: puede leer cualquier recurso para hacer refresh,
# no puede escribir ninguno ni acceder a versiones de secretos. No incluye
# storage.buckets.getIamPolicy, necesario para refrescar los bindings del bucket
# de estado; lo aporta securityReviewer (solo *.list y *.getIamPolicy, sin set).
resource "google_project_iam_member" "tf_plan_viewer" {
  # checkov:skip=CKV_GCP_117: viewer es exactamente el alcance buscado (lectura de todo para refresh del plan); un rol custom habría que mantenerlo recurso a recurso
  for_each = toset([
    "roles/viewer",
    "roles/iam.securityReviewer",
  ])

  project = var.project_id
  role    = each.value
  member  = google_service_account.tf_plan.member
}

# Lectura del estado de todas las capas (los dos buckets). Sin escritura: por
# eso los plan de PR y drift van con -lock=false (no puede crear el .tflock).
resource "google_storage_bucket_iam_member" "tf_plan_state" {
  for_each = toset([var.state_bucket, var.state_bucket_app])

  bucket = each.value
  role   = "roles/storage.objectViewer"
  member = google_service_account.tf_plan.member
}

moved {
  from = google_storage_bucket_iam_member.tf_plan_state
  to   = google_storage_bucket_iam_member.tf_plan_state["fixcomap-core-tfstate"]
}

# Cualquier rama del repo. Es seguro porque la SA no puede modificar nada.
resource "google_service_account_iam_member" "tf_plan_wif" {
  service_account_id = google_service_account.tf_plan.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository/${local.repo}"
}

# El binding de viewer pasó de recurso único a for_each; sin esto el plan lo
# destruiría y recrearía (ventana sin permisos para tf-plan).
moved {
  from = google_project_iam_member.tf_plan_viewer
  to   = google_project_iam_member.tf_plan_viewer["roles/viewer"]
}
