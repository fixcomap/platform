# Domain mapping de Cloud Run: gratis, certificado gestionado por Google. El
# CNAME correspondiente vive en infra/dns (Cloudflare) porque su credencial
# solo está disponible bajo el environment "platform".
resource "google_cloud_run_domain_mapping" "app" {
  count = var.enable_domain_mapping ? 1 : 0

  name     = var.app_hostname
  location = var.region

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_v2_service.app.name
  }
}
