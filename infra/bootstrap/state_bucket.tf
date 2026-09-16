# Bucket de estado de todas las capas. Coste: centimos/mes (fuera del free tier
# de GCS, que solo cubre regiones US; la latencia del estado no justifica cruzar
# el Atlántico ni el ruido de mezclar regiones).
resource "google_storage_bucket" "tfstate" {
  # checkov:skip=CKV_GCP_62: los access logs exigen un segundo bucket y Data Access audit logs con coste; los Admin Activity logs (gratis) ya registran cambios de IAM y de bucket
  name     = var.state_bucket
  location = var.region

  # El estado contiene IDs de recursos y outputs; nunca debe ser público ni
  # depender de ACLs por objeto.
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # Versionado: un apply corrupto o un estado sobrescrito se recupera con un
  # rollback de objeto. Se conservan 10 versiones para que no crezca sin control.
  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      num_newer_versions = 10
      with_state         = "ARCHIVED"
    }
    action {
      type = "Delete"
    }
  }

  # Sin force_destroy: perder el estado de todas las capas no puede ser un
  # efecto colateral de un destroy.
  force_destroy = false

  depends_on = [google_project_service.bootstrap]
}

# Estado de L2, aparte. Mismos controles que el principal. Lo crea tf-platform
# desde el pipeline, por eso depende de su storage.admin a nivel de proyecto.
resource "google_storage_bucket" "tfstate_app" {
  # checkov:skip=CKV_GCP_62: mismo motivo que el bucket principal
  name     = var.state_bucket_app
  location = var.region

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      num_newer_versions = 10
      with_state         = "ARCHIVED"
    }
    action {
      type = "Delete"
    }
  }

  force_destroy = false

  depends_on = [
    google_project_service.bootstrap,
    google_project_iam_member.tf_platform["roles/storage.admin"],
  ]
}
