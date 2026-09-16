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
  default     = "app.fixcomap.com"
}

variable "alert_email" {
  description = "Destino de las alertas de disponibilidad. Cloud Monitoring no verifica emails; comprobar que llega la primera."
  type        = string
  default     = "billing@fixcomap.com"
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
