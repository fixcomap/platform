variable "grafana_url" {
  description = "URL del stack de Grafana Cloud (https://<slug>.grafana.net)."
  type        = string
}

variable "stack_slug" {
  description = "Slug del stack; los data sources gestionados se llaman grafanacloud-<slug>-prom y -traces."
  type        = string
}

variable "service_name" {
  description = "service.name que exporta la app (label job en Mimir)."
  type        = string
  default     = "app"
}

variable "alert_email" {
  description = "Destino de las alertas de Grafana. Grafana Cloud solo acepta emails de usuarios de la organización (el buzón compartido billing@ no lo es); se pasa desde la variable de repo GRAFANA_ALERT_EMAIL."
  type        = string
}

variable "slo_availability" {
  description = "Objetivo de disponibilidad (fracción de peticiones sin 5xx) sobre 30 días."
  type        = number
  default     = 0.995
}

variable "slo_latency_p95_seconds" {
  description = "Objetivo de latencia: p95 por debajo de este valor."
  type        = number
  default     = 0.3
}
