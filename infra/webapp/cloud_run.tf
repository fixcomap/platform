# L2 genérico: un servicio Cloud Run de una webapp del catálogo, en un entorno.
# Lo aplica cicd.yml (gh-deployer) desde el repo de la webapp, con estado
# prefix=<service> en el bucket de L2. Todo sale de catalog/<webapp>.yaml; aquí
# no hay nada específico de ninguna app. IAM, secretos y hostnames viven en L1
# (infra/platform/webapps.tf): L2 no toca IAM.
#
# Contrato con la app: escucha en 8080, responde 200 en /health, lee
# APP_CONFIG (JSON) y la cadena de conexión en DATABASE_URL_FILE.
#
# Facturación por petición (cpu_idle) y escala a cero. Free tier: 2M peticiones,
# 360k GB-s y 180k vCPU-s al mes; con este tamaño y sin tráfico real el coste es 0.
module "catalog" {
  source = "../../modules/catalog"
  dir    = "${path.module}/../../catalog"
}

locals {
  svc        = module.catalog.webapp_services[var.service]
  production = local.svc.environment == "production"
}

resource "google_cloud_run_v2_service" "service" {
  name     = var.service
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  # Sin datos en el servicio: debe poder destruirse desde el pipeline.
  deletion_protection = false

  template {
    service_account = "${local.svc.runtime_sa}@${var.project_id}.iam.gserviceaccount.com"

    # Secreto como volumen: con version = "latest" Cloud Run lo resuelve al
    # arrancar cada instancia, así que una versión nueva entra sola al escalar
    # desde cero, sin redesplegar.
    volumes {
      name = "database-url"
      secret {
        secret = local.svc.database
        items {
          version = "latest"
          path    = "database-url"
        }
      }
    }

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

      # El primer hostname es el canónico: callback OAuth y destino de la
      # redirección del resto.
      env {
        name  = "APP_BASE_URL"
        value = "https://${local.svc.hostnames[0]}"
      }

      dynamic "env" {
        for_each = local.svc.db_name != "" ? [local.svc.db_name] : []
        content {
          name  = "DB_NAME"
          value = env.value
        }
      }

      env {
        name = "APP_CONFIG"
        value_source {
          secret_key_ref {
            secret  = local.svc.config
            version = "latest"
          }
        }
      }

      env {
        name  = "DATABASE_URL_FILE"
        value = "/secrets/database-url"
      }

      volume_mounts {
        name       = "database-url"
        mount_path = "/secrets"
      }

      # OpenTelemetry → Grafana Cloud por OTLP/HTTP con las variables estándar
      # del SDK: la app no conoce el backend. Endpoint vacío = no exporta.
      env {
        name  = "OTEL_EXPORTER_OTLP_ENDPOINT"
        value = local.production ? var.otlp_endpoint : ""
      }

      env {
        name  = "OTEL_EXPORTER_OTLP_PROTOCOL"
        value = "http/protobuf"
      }

      env {
        name  = "OTEL_SERVICE_NAME"
        value = var.service
      }

      env {
        name  = "OTEL_RESOURCE_ATTRIBUTES"
        value = "deployment.environment=${local.svc.environment},cloud.provider=gcp,cloud.region=${var.region},service.version=${var.image}"
      }

      env {
        name = "OTEL_EXPORTER_OTLP_HEADERS"
        value_source {
          secret_key_ref {
            secret  = var.otlp_headers_secret_id
            version = "latest"
          }
        }
      }

      ports {
        container_port = 8080
      }

      startup_probe {
        http_get {
          path = "/health"
        }
        initial_delay_seconds = 0
        period_seconds        = 5
        failure_threshold     = 3
      }
    }
  }
}
