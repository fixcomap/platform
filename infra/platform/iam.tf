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

# Estado de L2 y nada más. La condición limita el binding al prefijo app. GCS
# evalúa resource.name también en las llamadas list (contra el parámetro prefix
# de la petición); el backend gcs lista con prefix=app, sin barra final, así que
# la condición no puede llevarla. Ningún otro prefijo del bucket empieza por "app".
resource "google_storage_bucket_iam_member" "gh_deployer_state" {
  bucket = var.state_bucket
  role   = "roles/storage.objectUser"
  member = google_service_account.gh_deployer.member

  condition {
    title       = "solo-prefijo-app"
    description = "Estado de L2 únicamente; L0 y L1 son de tf-platform."
    expression  = "resource.name.startsWith(\"projects/_/buckets/${var.state_bucket}/objects/app\")"
  }
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
# no puede escribir ninguno ni acceder a versiones de secretos.
resource "google_project_iam_member" "tf_plan_viewer" {
  # checkov:skip=CKV_GCP_117: viewer es exactamente el alcance buscado (lectura de todo para refresh del plan); un rol custom habría que mantenerlo recurso a recurso
  project = var.project_id
  role    = "roles/viewer"
  member  = google_service_account.tf_plan.member
}

# Lectura del estado de todas las capas. Sin escritura: por eso los plan de PR
# y drift van con -lock=false (no puede crear el objeto .tflock).
resource "google_storage_bucket_iam_member" "tf_plan_state" {
  bucket = var.state_bucket
  role   = "roles/storage.objectViewer"
  member = google_service_account.tf_plan.member
}

# Cualquier rama del repo. Es seguro porque la SA no puede modificar nada.
resource "google_service_account_iam_member" "tf_plan_wif" {
  service_account_id = google_service_account.tf_plan.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository/${local.repo}"
}
