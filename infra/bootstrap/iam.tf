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
    "roles/storage.admin",                   # crear los buckets de estado y fijar sus bindings; en el proyecto solo hay buckets de estado
    "roles/run.admin",                       # IAM del servicio Cloud Run (run.invoker a allUsers); gh-deployer solo es run.developer
  ])

  project = var.project_id
  role    = each.value
  member  = google_service_account.tf_platform.member
}

# El acceso a los buckets de estado lo da storage.admin a nivel de proyecto
# (arriba): tf-platform tiene que poder CREAR el bucket de L2 desde el pipeline
# y eso no se concede a nivel de bucket. El binding por bucket que había aquí
# se ha eliminado; el plan lo destruye.

# Solo un job con environment "platform", desde main, del repo platform, puede
# suplantar a tf-platform. Cualquier otra combinación no obtiene token.
resource "google_service_account_iam_member" "tf_platform_wif" {
  service_account_id = google_service_account.tf_platform.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repo_ref_env/${var.github_org}/${var.github_repo}:refs/heads/main:environment:platform"
}
