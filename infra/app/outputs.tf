output "service_url" {
  description = "URL *.run.app del servicio."
  value       = google_cloud_run_v2_service.app.uri
}
