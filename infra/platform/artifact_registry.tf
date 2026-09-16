# Repositorio Docker. Free tier: 0.5 GB en total; a partir de ahí $0.10/GB/mes.
# Las políticas de limpieza son la única forma de que no crezca con cada merge.
resource "google_artifact_registry_repository" "docker" {
  # checkov:skip=CKV_GCP_84: CMEK requiere Cloud KMS ($0.06/clave/mes + operaciones) para imágenes públicas de una PoC; sin justificación de coste
  location      = var.region
  repository_id = "docker"
  format        = "DOCKER"
  description   = "Imágenes de la plataforma"

  # No se activa vulnerability_scanning_config: Artifact Analysis cobra $0.26
  # por imagen escaneada. El escaneo lo hace trivy en el pipeline, gratis.

  # dry_run = false: las políticas borran de verdad. Con true solo loguean.
  cleanup_policy_dry_run = false

  # KEEP tiene prioridad sobre DELETE: las 5 versiones más recientes de cada
  # paquete sobreviven aunque cumplan las condiciones de borrado.
  cleanup_policies {
    id     = "keep-latest-5"
    action = "KEEP"
    most_recent_versions {
      keep_count = 5
    }
  }

  # Manifiestos sin tag (capas huérfanas, attestations) no sirven a nadie
  # pasado un día.
  cleanup_policies {
    id     = "delete-untagged-1d"
    action = "DELETE"
    condition {
      tag_state  = "UNTAGGED"
      older_than = "1d"
    }
  }

  # Cualquier imagen con tag de más de 30 días que no esté entre las 5 últimas.
  cleanup_policies {
    id     = "delete-tagged-30d"
    action = "DELETE"
    condition {
      tag_state  = "TAGGED"
      older_than = "30d"
    }
  }

  depends_on = [google_project_service.platform]
}
