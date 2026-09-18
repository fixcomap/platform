# Secreto de configuración de la app. El contenedor de Secret Manager y su IAM
# viven aquí (L1) porque conceder acceso es IAM; L2 solo lo referencia por nombre.
# Free tier: 6 versiones activas y 10k accesos/mes.
resource "google_secret_manager_secret" "app_config" {
  secret_id = "app-config"

  replication {
    auto {}
  }

  depends_on = [google_project_service.platform]
}

# Versión inicial con un placeholder: Cloud Run rechaza el despliegue si el
# secreto no tiene ninguna versión. Write-only: el valor no entra en el estado.
# El valor real se sube con gcloud fuera del repo; esta versión queda como
# la primera del historial y no se vuelve a tocar salvo que cambie _wo_version.
resource "google_secret_manager_secret_version" "app_config_placeholder" {
  secret                 = google_secret_manager_secret.app_config.id
  secret_data_wo         = "placeholder"
  secret_data_wo_version = "1"
}

# Cadena de conexión a Neon (Postgres serverless, free tier). Secreto propio y no
# app-config: distinto ciclo de vida y así el accessor de app-runtime se ve
# recurso a recurso. El valor real se sube con gcloud fuera del repo.
resource "google_secret_manager_secret" "database_url" {
  secret_id = "database-url"

  replication {
    auto {}
  }

  depends_on = [google_project_service.platform]
}

# Placeholder para que Cloud Run acepte el montaje antes de que exista el valor
# real. Write-only: no entra en el estado.
resource "google_secret_manager_secret_version" "database_url_placeholder" {
  secret                 = google_secret_manager_secret.database_url.id
  secret_data_wo         = "postgres://placeholder"
  secret_data_wo_version = "1"
}

# Cabeceras OTLP para Grafana Cloud: "Authorization=Basic <base64(instanceID:token)>".
# Secreto propio porque es una credencial de un tercero con su propia rotación.
# El valor real se sube con gcloud fuera del repo (README, Observabilidad).
resource "google_secret_manager_secret" "otlp_headers" {
  secret_id = "otlp-headers"

  replication {
    auto {}
  }

  depends_on = [google_project_service.platform]
}

resource "google_secret_manager_secret_version" "otlp_headers_placeholder" {
  secret                 = google_secret_manager_secret.otlp_headers.id
  secret_data_wo         = "Authorization=Basic placeholder"
  secret_data_wo_version = "1"
}
