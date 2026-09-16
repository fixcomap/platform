output "service_url" {
  description = "URL *.run.app del servicio."
  value       = google_cloud_run_v2_service.app.uri
}

output "app_url" {
  value = var.enable_domain_mapping ? "https://${var.app_hostname}" : google_cloud_run_v2_service.app.uri
}
