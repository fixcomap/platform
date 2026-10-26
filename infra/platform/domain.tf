# Domain mapping de Cloud Run: gratis, certificado gestionado por Google. Vive
# en L1 y no en L2 porque run.domainmappings.* solo lo aporta run.admin (no
# existe en run.developer ni puede ir en un rol custom), y porque exponer un
# hostname es, como el invoker, una decisión de plataforma. tf-platform debe
# ser propietario verificado del dominio en Search Console (ver README).
# El CNAME correspondiente vive en infra/dns.

# Mappings de app y app-dev, ahora de plantilla y plantilla-dev (webapps.tf).
moved {
  from = google_cloud_run_domain_mapping.app["app.fixcomap.com"]
  to   = google_cloud_run_domain_mapping.webapp["app.fixcomap.com"]
}
moved {
  from = google_cloud_run_domain_mapping.app["plantilla.fixcomap.com"]
  to   = google_cloud_run_domain_mapping.webapp["plantilla.fixcomap.com"]
}
moved {
  from = google_cloud_run_domain_mapping.app_dev["dev.fixcomap.com"]
  to   = google_cloud_run_domain_mapping.webapp["dev.fixcomap.com"]
}
moved {
  from = google_cloud_run_domain_mapping.app_dev["plantilla.dev.fixcomap.com"]
  to   = google_cloud_run_domain_mapping.webapp["plantilla.dev.fixcomap.com"]
}
