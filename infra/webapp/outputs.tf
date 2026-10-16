output "service_url" {
  description = "URL *.run.app del servicio."
  value       = google_cloud_run_v2_service.service.uri
}

output "public_url" {
  description = "URL canónica (primer hostname del catálogo); solo responde con public: true."
  value       = "https://${local.svc.hostnames[0]}"
}

output "public" {
  value = local.svc.public
}
