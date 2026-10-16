variable "project_id" {
  type    = string
  default = "fixcomap-core"
}

variable "region" {
  type    = string
  default = "europe-west1"
}

variable "service" {
  description = "Servicio Cloud Run a desplegar, tal como lo deriva el catálogo: <service> (producción) o <service>-dev."
  type        = string
}

variable "image" {
  description = "Imagen por digest (…@sha256:…). La fija cicd.yml tras el push."
  type        = string
}

variable "otlp_endpoint" {
  description = "Endpoint OTLP/HTTP de Grafana Cloud; solo producción exporta telemetría (desarrollo no gasta cuota)."
  type        = string
  default     = "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"
}

variable "otlp_headers_secret_id" {
  description = "Secreto compartido de L1 con las cabeceras OTLP (Authorization=Basic …)."
  type        = string
  default     = "otlp-headers"
}
