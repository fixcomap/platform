# Workload Identity Federation: GitHub Actions intercambia su token OIDC por
# credenciales de corta duración. Es la razón de que no exista ninguna clave JSON.
resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"

  depends_on = [google_project_service.bootstrap]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  # checkov:skip=CKV_GCP_125: el check exige un literal assertion.sub == "repo:..."; aquí el sub varía por environment/ref y la restricción por repo+rama+environment se hace en los bindings principalSet de cada SA
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-oidc"
  display_name                       = "GitHub OIDC"

  # Primera barrera: ningún token de fuera de la organización pasa de aquí,
  # aunque alguien consiga un binding erróneo más abajo.
  attribute_condition = "assertion.repository_owner == \"${var.github_org}\""

  # Los bindings por SA (principalSet://...) solo pueden filtrar por UN atributo
  # mapeado. Por eso las condiciones compuestas se materializan como atributos:
  #   repository     -> tf-plan     (cualquier rama, solo lectura)
  #   repository_ref -> gh-deployer (repo + rama main)
  #   sub_ref        -> tf-platform (repo + environment "platform" + rama main;
  #                     el sub de un job con environment es
  #                     repo:ORG/REPO:environment:NOMBRE y el claim ref sigue presente)
  attribute_mapping = {
    "google.subject"           = "assertion.sub"
    "attribute.repository"     = "assertion.repository"
    "attribute.repository_ref" = "assertion.repository + \"@\" + assertion.ref"
    "attribute.sub_ref"        = "assertion.sub + \"@\" + assertion.ref"
    "attribute.actor"          = "assertion.actor"
  }

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}
