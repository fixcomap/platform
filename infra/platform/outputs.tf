output "gh_deployer_email" {
  description = "SA que usa apply-app.yml."
  value       = google_service_account.gh_deployer.email
}

output "tf_plan_email" {
  description = "SA que usan pr-checks.yml y drift.yml."
  value       = google_service_account.tf_plan.email
}

output "app_runtime_email" {
  description = "Identidad del servicio Cloud Run; L2 la pasa como service_account."
  value       = google_service_account.app_runtime.email
}

output "docker_repository" {
  description = "Prefijo de las imágenes: <region>-docker.pkg.dev/<proyecto>/docker."
  value       = "${google_artifact_registry_repository.docker.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.docker.repository_id}"
}

output "app_config_secret_id" {
  description = "Nombre del secreto que L2 monta en Cloud Run."
  value       = google_secret_manager_secret.app_config.secret_id
}
