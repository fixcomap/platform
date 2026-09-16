# Cloud Run, facturación por petición (cpu_idle = true) y escala a cero.
# Free tier: 2M peticiones, 360k GB-s y 180k vCPU-s al mes. Con este tamaño y
# sin tráfico real el coste es 0.
resource "google_cloud_run_v2_service" "app" {
  name     = var.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  # Sin invoker_iam_disabled: Cloud Run exige run.services.setIamPolicy para
  # cambiarlo, es decir, IAM. La exposición pública (run.invoker a allUsers y el
  # domain mapping) vive en L1 (cloud_run_iam.tf, domain.tf) y la aplica
  # tf-platform. L2 no toca IAM ni hostnames.

  # PoC sin datos: el servicio debe poder destruirse desde el pipeline.
  deletion_protection = false

  template {
    service_account = var.app_runtime_email

    # max 2: tope de gasto ante un pico o un bucle de reintentos.
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image = var.image

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      env {
        name  = "APP_VERSION"
        value = var.image
      }

      # Secreto de L1 como variable de entorno. app-runtime tiene secretAccessor
      # sobre él (L1); si no, Cloud Run rechaza la revisión.
      env {
        name = "APP_CONFIG"
        value_source {
          secret_key_ref {
            secret  = var.app_config_secret_id
            version = "latest"
          }
        }
      }

      ports {
        container_port = 8080
      }

      startup_probe {
        http_get {
          path = "/healthz"
        }
        initial_delay_seconds = 0
        period_seconds        = 5
        failure_threshold     = 3
      }
    }
  }
}
