# Lo que necesita cada webapp del catálogo (catalog/<k>.yaml, type webapp) y
# solo L1 puede crear, porque es IAM: identidad de ejecución, secretos, acceso
# de su repo a gh-deployer y la exposición pública. El servicio en sí lo crea
# infra/webapp (L2) desde el pipeline estándar.
#
# Alta en dos releases: con public: false se crea todo menos run.invoker y los
# domain mappings, que exigen que el servicio exista; tras el primer deploy de
# cada entorno, public: true.
module "catalog" {
  source = "../../modules/catalog"
  dir    = "${path.module}/../../catalog"
}

locals {
  webapps          = module.catalog.webapps
  webapp_services  = module.catalog.webapp_services
  webapp_hostnames = module.catalog.webapp_hostnames

  # Ramas de cada repo de webapp que pueden suplantar a gh-deployer.
  webapp_deploy_refs = merge([
    for k, w in local.webapps : {
      for ref in ["main", "develop"] : "${k}:${ref}" => "${var.github_org}/${k}:refs/heads/${ref}"
    }
  ]...)

  # Secretos que monta cada servicio; otlp-headers es compartido.
  webapp_secret_access = merge([
    for k, w in local.webapps : {
      "${k}:config"   = { webapp = k, secret = google_secret_manager_secret.webapp_config[k].id }
      "${k}:database" = { webapp = k, secret = google_secret_manager_secret.webapp_database_url[k].id }
      "${k}:otlp"     = { webapp = k, secret = google_secret_manager_secret.otlp_headers.id }
    }
  ]...)
}

# ---------- identidad ----------

# Una por webapp, compartida por producción y desarrollo (mismo modelo que app-runtime).
resource "google_service_account" "webapp_runtime" {
  for_each = local.webapps

  account_id   = "${each.value.service}-runtime"
  display_name = "Cloud Run: ${each.key} (catalog/${each.key}.yaml)"
}

resource "google_service_account_iam_member" "gh_deployer_actas_webapp" {
  for_each = local.webapps

  service_account_id = google_service_account.webapp_runtime[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gh_deployer.member
}

# El repo de cada webapp despliega desde main y develop; nada más puede.
resource "google_service_account_iam_member" "gh_deployer_wif_webapp" {
  for_each = local.webapp_deploy_refs

  service_account_id = google_service_account.gh_deployer.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository_ref/${each.value}"
}

# ---------- secretos ----------
# Solo el contenedor con un placeholder: el valor real se carga a mano
# (gcloud secrets versions add) y nunca pasa por el repo ni por el estado.

resource "google_secret_manager_secret" "webapp_config" {
  for_each = local.webapps

  secret_id = "${each.value.service}-config"
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "webapp_config_placeholder" {
  for_each = local.webapps

  secret                 = google_secret_manager_secret.webapp_config[each.key].id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"
}

resource "google_secret_manager_secret" "webapp_database_url" {
  for_each = local.webapps

  secret_id = "${each.value.service}-database-url"
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "webapp_database_url_placeholder" {
  for_each = local.webapps

  secret                 = google_secret_manager_secret.webapp_database_url[each.key].id
  secret_data_wo         = "postgres://placeholder"
  secret_data_wo_version = "1"
}

resource "google_secret_manager_secret_iam_member" "webapp_runtime_access" {
  for_each = local.webapp_secret_access

  secret_id = each.value.secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.webapp_runtime[each.value.webapp].member
}

# ---------- exposición pública (public: true) ----------

resource "google_cloud_run_v2_service_iam_member" "webapp_public" {
  for_each = { for s, e in local.webapp_services : s => e if e.public }

  project  = var.project_id
  location = var.region
  name     = each.key
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# El CNAME de cada hostname lo crea infra/dns desde el mismo catálogo.
resource "google_cloud_run_domain_mapping" "webapp" {
  for_each = { for h, s in local.webapp_hostnames : h => s if local.webapp_services[s].public }

  name     = each.key
  location = var.region
  metadata {
    namespace = var.project_id
  }
  spec {
    route_name = each.value
  }
}
