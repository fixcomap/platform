# Domain mapping de Cloud Run: gratis, certificado gestionado por Google. Vive
# en L1 y no en L2 porque run.domainmappings.* solo lo aporta run.admin (no
# existe en run.developer ni puede ir en un rol custom), y porque exponer un
# hostname es, como el invoker, una decisión de plataforma. tf-platform debe
# ser propietario verificado del dominio en Search Console (ver README).
# El CNAME correspondiente vive en infra/dns.
moved {
  from = google_cloud_run_domain_mapping.app[0]
  to   = google_cloud_run_domain_mapping.app["app.fixcomap.com"]
}

resource "google_cloud_run_domain_mapping" "app" {
  for_each = var.enable_domain_mapping ? var.app_hostnames : []

  name     = each.value
  location = var.region

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = var.cloud_run_service_name
  }
}

moved {
  from = google_cloud_run_domain_mapping.app_dev[0]
  to   = google_cloud_run_domain_mapping.app_dev["dev.fixcomap.com"]
}

resource "google_cloud_run_domain_mapping" "app_dev" {
  for_each = var.enable_dev_domain_mapping ? var.dev_hostnames : []

  name     = each.value
  location = var.region

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = var.dev_service_name
  }
}
