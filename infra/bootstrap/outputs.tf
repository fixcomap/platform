output "workload_identity_provider" {
  description = "Valor de la variable GCP_WIF_PROVIDER en GitHub (google-github-actions/auth)."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "workload_identity_pool" {
  description = "Nombre completo del pool; L1 lo reconstruye a partir del número de proyecto."
  value       = google_iam_workload_identity_pool.github.name
}

output "tf_platform_email" {
  description = "SA que usa apply-platform.yml."
  value       = google_service_account.tf_platform.email
}

output "state_bucket_app" {
  description = "Bucket de estado de L2; -backend-config=bucket=... en infra/app."
  value       = google_storage_bucket.tfstate_app.name
}

output "state_bucket" {
  description = "Bucket de estado; -backend-config=bucket=... en todas las capas."
  value       = google_storage_bucket.tfstate.name
}
