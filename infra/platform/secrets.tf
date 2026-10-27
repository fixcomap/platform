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
