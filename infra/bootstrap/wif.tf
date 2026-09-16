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
  # aunque alguien consiga un binding erróneo más abajo. Nombre e ID: el nombre
  # es legible, el ID no se recicla.
  attribute_condition = "assertion.repository_owner == \"${var.github_org}\" && assertion.repository_owner_id == \"${var.github_org_id}\""

  # Los bindings por SA (principalSet://...) solo pueden filtrar por UN atributo
  # mapeado. Por eso las condiciones compuestas se materializan como atributos:
  #   repository     -> tf-plan     (cualquier rama, solo lectura)
  #   repository_ref -> gh-deployer (repo + rama main)
  #   repo_ref_env   -> tf-platform (repo + rama main + environment "platform")
  #
  # No se usa assertion.sub literal: los repos creados después del 15/07/2026
  # emiten repo:ORG@ID/REPO@ID:environment:NOMBRE (formato "inmutable" de GitHub)
  # y el anterior era repo:ORG/REPO:environment:NOMBRE. Ambos terminan en
  # ":environment:NOMBRE", así que el environment se extrae del sub con funciones
  # que Google documenta para attribute mappings (contains, split, ternario).
  # repository y ref siguen siendo claims por nombre en ambos formatos.
  # Separador ":" y no "@": es el que GitHub ya usa en el sub y Google acepta en
  # los members de un principalSet.
  attribute_mapping = {
    "google.subject"           = "assertion.sub"
    "attribute.repository"     = "assertion.repository"
    "attribute.repository_ref" = "assertion.repository + \":\" + assertion.ref"
    "attribute.repo_ref_env"   = "assertion.repository + \":\" + assertion.ref + \":environment:\" + (assertion.sub.contains(\":environment:\") ? assertion.sub.split(\":environment:\")[1] : \"\")"
    "attribute.actor"          = "assertion.actor"
  }

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}
