# Domain mapping de Cloud Run: gratis, certificado gestionado por Google. Vive
# en L1 y no en L2 porque run.domainmappings.* solo lo aporta run.admin (no
# existe en run.developer ni puede ir en un rol custom), y porque exponer un
# hostname es, como el invoker, una decisión de plataforma. tf-platform debe
# ser propietario verificado del dominio en Search Console (ver README).
# El CNAME correspondiente vive en infra/dns.
resource "google_cloud_run_domain_mapping" "app" {
  count = var.enable_domain_mapping ? 1 : 0

  name     = var.app_hostname
  location = var.region

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = var.cloud_run_service_name
  }
}
