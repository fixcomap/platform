# Backups de las bases de producción de cada webapp (backups.yml): pg_dump
# diario a un bucket propio. db-backups lee solo esas cadenas y solo puede
# crear objetos; la retención la aplica el bucket.
locals {
  # Producción: la cadena directa de webapp.migrate o, en el contrato original,
  # <service>-database-url (backups.yml quita el -pooler del host).
  backup_secrets = merge(
    { for s, e in local.webapp_services : s => google_secret_manager_secret.webapp_migrate["${s}-database-url-direct"].id if e.environment == "production" && e.migrate != "" },
    { for k, w in local.webapps_legacy : w.service => google_secret_manager_secret.webapp_database_url[k].id if w.migrate == "" },
  )
}

resource "google_storage_bucket" "backups" {
  # checkov:skip=CKV_GCP_78: db-backups no puede sobrescribir ni borrar; cada dump es un objeto nuevo y la retención es la del lifecycle
  # checkov:skip=CKV_GCP_62: mismo motivo que los buckets de estado (access logs con coste; Admin Activity ya registra cambios)
  name                        = "${var.project_id}-backups"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type = "Delete"
    }
  }
}

resource "google_service_account" "db_backups" {
  account_id   = "db-backups"
  display_name = "backups.yml: pg_dump de producción al bucket de backups"
}

resource "google_storage_bucket_iam_member" "db_backups_create" {
  bucket = google_storage_bucket.backups.name
  role   = "roles/storage.objectCreator"
  member = google_service_account.db_backups.member
}

resource "google_secret_manager_secret_iam_member" "db_backups_read" {
  for_each = local.backup_secrets

  secret_id = each.value
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.db_backups.member
}

# Solo desde develop, la rama por defecto: ahí corre el schedule.
resource "google_service_account_iam_member" "db_backups_wif" {
  service_account_id = google_service_account.db_backups.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${local.wif_pool}/attribute.repository_ref/${local.repo}:refs/heads/develop"
}
