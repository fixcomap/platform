# gh-deployer: publica imágenes y aplica L2 (Cloud Run + DNS). Sin permisos IAM.
resource "google_service_account" "gh_deployer" {
  account_id   = "gh-deployer"
  display_name = "GitHub Actions: build, push y apply de L2"
}

# app-runtime: identidad con la que corre la app. Separada del deployer para
# que un compromiso del contenedor no permita publicar imágenes ni tocar infra.
resource "google_service_account" "app_runtime" {
  account_id   = "app-runtime"
  display_name = "Identidad de ejecución de Cloud Run"
}

# tf-plan: solo lectura. Con ella se hace plan en PRs (cualquier rama) y el
# cron de drift. No puede modificar nada, así que un PR malicioso no escala.
resource "google_service_account" "tf_plan" {
  account_id   = "tf-plan"
  display_name = "OpenTofu plan (solo lectura)"
}
