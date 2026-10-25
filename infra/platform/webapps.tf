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

  # Contrato original (plantilla): <service>-config y <service>-database-url,
  # compartidos por producción y desarrollo.
  webapps_legacy = { for k, w in local.webapps : k => w if w.legacy }

  # webapp.secrets: un secreto por variable y por servicio (cada entorno con
  # los suyos: base, credenciales de pago de prueba o reales...).
  webapp_env_secrets = merge([
    for s, e in local.webapp_services : {
      for name, id in e.secrets : id => { webapp = e.webapp, service = s, variable = name }
    }
  ]...)

  # webapp.migrate: cadena directa (sin pooler) para las migraciones, que lee
  # el pipeline (gh-deployer) y no el servicio.
  webapp_migrate_secrets = {
    for s, e in local.webapp_services : "${s}-database-url-direct" => { webapp = e.webapp, service = s } if e.migrate != ""
  }

  # Secretos que monta cada servicio; otlp-headers es compartido.
  webapp_secret_access = merge(
    merge([
      for k, w in local.webapps_legacy : {
        "${k}:config"   = { webapp = k, secret = google_secret_manager_secret.webapp_config[k].id }
        "${k}:database" = { webapp = k, secret = google_secret_manager_secret.webapp_database_url[k].id }
      }
    ]...),
    { for k, w in local.webapps : "${k}:otlp" => { webapp = k, secret = google_secret_manager_secret.otlp_headers.id } },
    { for id, x in local.webapp_env_secrets : "${x.service}:${x.variable}" => { webapp = x.webapp, secret = google_secret_manager_secret.webapp_env[id].id } },
    { for s, e in local.webapp_services : "${s}:acceso" => { webapp = e.webapp, secret = google_secret_manager_secret.dev_acceso.id } if e.restricted },
  )
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
  for_each = local.webapps_legacy

  secret_id = "${each.value.service}-config"
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "webapp_config_placeholder" {
  for_each = local.webapps_legacy

  secret                 = google_secret_manager_secret.webapp_config[each.key].id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"

  # Una vez cargado el valor real, el placeholder se desactiva a mano: no reactivarlo.
  lifecycle {
    ignore_changes = [enabled]
  }
}

resource "google_secret_manager_secret" "webapp_database_url" {
  for_each = local.webapps_legacy

  secret_id = "${each.value.service}-database-url"
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "webapp_database_url_placeholder" {
  for_each = local.webapps_legacy

  secret                 = google_secret_manager_secret.webapp_database_url[each.key].id
  secret_data_wo         = "postgres://placeholder"
  secret_data_wo_version = "1"

  # Una vez cargado el valor real, el placeholder se desactiva a mano: no reactivarlo.
  lifecycle {
    ignore_changes = [enabled]
  }
}

resource "google_secret_manager_secret" "webapp_env" {
  for_each = local.webapp_env_secrets

  secret_id = each.key
  labels    = { webapp = each.value.webapp, service = each.value.service }
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

# Placeholder que no parece válido a propósito: una app que valida su
# configuración al arrancar falla hasta que se cargue el valor real.
resource "google_secret_manager_secret_version" "webapp_env_placeholder" {
  for_each = local.webapp_env_secrets

  secret                 = google_secret_manager_secret.webapp_env[each.key].id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"

  # Una vez cargado el valor real, el placeholder se desactiva a mano: no reactivarlo.
  lifecycle {
    ignore_changes = [enabled]
  }
}

resource "google_secret_manager_secret" "webapp_migrate" {
  for_each = local.webapp_migrate_secrets

  secret_id = each.key
  labels    = { webapp = each.value.webapp, service = each.value.service }
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "webapp_migrate_placeholder" {
  for_each = local.webapp_migrate_secrets

  secret                 = google_secret_manager_secret.webapp_migrate[each.key].id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"

  # Una vez cargado el valor real, el placeholder se desactiva a mano: no reactivarlo.
  lifecycle {
    ignore_changes = [enabled]
  }
}

# Lo lee el job de deploy para correr webapp.migrate antes del apply de L2.
resource "google_secret_manager_secret_iam_member" "gh_deployer_migrate" {
  for_each = local.webapp_migrate_secrets

  secret_id = google_secret_manager_secret.webapp_migrate[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.gh_deployer.member
}

resource "google_secret_manager_secret_iam_member" "webapp_runtime_access" {
  for_each = local.webapp_secret_access

  secret_id = each.value.secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.webapp_runtime[each.value.webapp].member
}

# Emails que pueden entrar a los entornos de desarrollo restringidos, separados
# por coma: lo lee infra/dns (Cloudflare Access del front) y cada API de dev
# (ACCESO_EMAILS). Compartido por todas las webapps; el valor se carga a mano.
resource "google_secret_manager_secret" "dev_acceso" {
  secret_id = "dev-acceso-emails"
  replication {
    auto {}
  }
  depends_on = [google_project_service.platform]
}

# Sin "@": infra/dns no crea Access y las APIs no restringen hasta el valor real.
resource "google_secret_manager_secret_version" "dev_acceso_placeholder" {
  secret                 = google_secret_manager_secret.dev_acceso.id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"

  # Una vez cargado el valor real, el placeholder se desactiva a mano: no reactivarlo.
  lifecycle {
    ignore_changes = [enabled]
  }
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
