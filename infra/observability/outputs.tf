output "dashboard_url" {
  description = "Dashboard app / overview en Grafana Cloud."
  value       = "${var.grafana_url}/d/${grafana_dashboard.app.uid}"
}
