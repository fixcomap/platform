# Exposición pública del servicio Cloud Run: run.invoker para allUsers. Es la
# única pieza de IAM del servicio y por eso la gestiona tf-platform, no
# gh-deployer. Sin coste: Cloud Run factura por petición (free tier 2M/mes).
resource "google_cloud_run_v2_service_iam_member" "app_public" {
  # checkov:skip=CKV_GCP_102: acceso público deliberado; PoC sin datos, cpu_idle y max 2 instancias acotan el gasto
  count = var.app_public ? 1 : 0

  project  = var.project_id
  location = var.region
  name     = var.cloud_run_service_name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
