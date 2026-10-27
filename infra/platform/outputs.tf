output "gh_deployer_email" {
  description = "SA que usa apply-app.yml."
  value       = google_service_account.gh_deployer.email
}

output "tf_plan_email" {
  description = "SA que usan pr-checks.yml y drift.yml."
  value       = google_service_account.tf_plan.email
}

output "docker_repository" {
  description = "Prefijo de las imágenes: <region>-docker.pkg.dev/<proyecto>/docker."
  value       = "${google_artifact_registry_repository.docker.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.docker.repository_id}"
}

