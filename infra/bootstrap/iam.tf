# tf-platform: la única identidad que puede crear IAM. Es Owner de facto, y por
# eso solo la usa apply-platform.yml, bajo environment con aprobación humana.
resource "google_service_account" "tf_platform" {
  account_id   = "tf-platform"
  display_name = "OpenTofu L0+L1 (pipeline, aprobación humana)"

  depends_on = [google_project_service.bootstrap]
}

# Roles a nivel de proyecto. Cada uno responde a un recurso concreto de L0/L1;
# si se añade un tipo de recurso nuevo, se añade el rol aquí y se justifica.
resource "google_project_iam_member" "tf_platform" {
  # checkov:skip=CKV_GCP_49: tf-platform es, por diseño, la única identidad que administra SAs; se mitiga con environment + aprobación humana y condición OIDC (repo+main+environment)
  for_each = toset([
    "roles/serviceusage.serviceUsageAdmin",  # habilitar APIs
    "roles/iam.serviceAccountAdmin",         # crear SAs y sus bindings (serviceAccountUser, workloadIdentityUser)
    "roles/resourcemanager.projectIamAdmin", # bindings a nivel de proyecto (viewer para tf-plan, run.developer para gh-deployer)
    "roles/iam.workloadIdentityPoolAdmin",   # gestionar el pool/provider de esta capa una vez migrada al pipeline
    "roles/artifactregistry.admin",          # repositorio Docker y su IAM
    "roles/secretmanager.admin",             # secretos y su IAM (nunca lee versiones: eso es secretAccessor)
  ])

  project = var.project_id
  role    = each.value
  member  = google_service_account.tf_platform.member
}

# Acceso completo al bucket de estado, a nivel de bucket y no de proyecto:
# necesita leer/escribir el estado de todas las capas y fijar los bindings por
# prefijo de tf-plan y gh-deployer (setIamPolicy sobre el bucket).
resource "google_storage_bucket_iam_member" "tf_platform_state" {
  bucket = google_storage_bucket.tfstate.name
  role   = "roles/storage.admin"
  member = google_service_account.tf_platform.member
}

# Solo un job con environment "platform", desde main, del repo platform, puede
# suplantar a tf-platform. Cualquier otra combinación no obtiene token.
resource "google_service_account_iam_member" "tf_platform_wif" {
  service_account_id = google_service_account.tf_platform.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.sub_ref/repo:${var.github_org}/${var.github_repo}:environment:platform@refs/heads/main"
}
