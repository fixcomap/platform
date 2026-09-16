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
