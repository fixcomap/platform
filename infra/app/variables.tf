variable "project_id" {
  type    = string
  default = "fixcomap-core"
}

variable "region" {
  type    = string
  default = "europe-west1"
}

variable "service_name" {
  type    = string
  default = "app"
}

variable "image" {
  description = <<-EOT
    Imagen a desplegar, por digest (…@sha256:…). La fija apply-app.yml tras el push;
    en PR y drift se lee del servicio desplegado para que el plan no muestre ruido.
  EOT
  type        = string
}

variable "app_runtime_email" {
  description = "SA creada en L1 con la que corre el servicio."
  type        = string
  default     = "app-runtime@fixcomap-core.iam.gserviceaccount.com"
}

variable "app_hostname" {
  description = "Hostname público que sondea el uptime check; el mapping vive en L1 y el CNAME en infra/dns."
  type        = string
  default     = "plantilla.fixcomap.com"
}

variable "landing_hostname" {
  description = "Hostname público de la landing (Cloudflare Pages, repo fixcomap/web); solo se sondea desde aquí."
  type        = string
  default     = "fixcomap.com"
}

variable "alert_email" {
  description = "Destino de las alertas de disponibilidad (secret ALERT_EMAIL)."
  type        = string
  default     = ""
  sensitive   = true
}

variable "otlp_endpoint" {
  description = "Endpoint OTLP/HTTP de Grafana Cloud (p.ej. https://otlp-gateway-prod-eu-west-2.grafana.net/otlp). Vacío = la app no exporta telemetría."
  type        = string
  default     = ""
}

variable "otlp_headers_secret_id" {
  description = "Secreto de L1 con las cabeceras OTLP (Authorization=Basic …), montado como OTEL_EXPORTER_OTLP_HEADERS."
  type        = string
  default     = "otlp-headers"
}

variable "database_url_secret_id" {
  description = "Secreto de L1 con la cadena de conexión; montado como fichero en /secrets/database-url."
  type        = string
  default     = "database-url"
}

variable "app_config_secret_id" {
  description = "Secreto de L1 montado como variable de entorno."
  type        = string
  default     = "app-config"
}

# ---------- entorno ----------
# La misma configuración despliega producción (estado prefix=app, defaults) y
# desarrollo (estado prefix=app-dev, deploy-dev.yml): solo cambian estas variables.

variable "environment" {
  description = "deployment.environment de OTel: production o development."
  type        = string
  default     = "production"
}

variable "db_name" {
  description = "Base de datos de Neon; vacío = la de la cadena de conexión (producción)."
  type        = string
  default     = ""
}

variable "base_url" {
  description = <<-EOT
    URL pública principal (APP_BASE_URL): callback OAuth y destino de la
    redirección del resto de hostnames. Prevalece sobre base_url de app-config.
  EOT
  type        = string
  default     = "https://plantilla.fixcomap.com"
}

variable "enable_monitoring" {
  description = "Uptime checks y alertas. Solo producción: desarrollo no avisa a nadie."
  type        = bool
  default     = true
}
