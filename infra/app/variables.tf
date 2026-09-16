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

variable "app_config_secret_id" {
  description = "Secreto de L1 montado como variable de entorno."
  type        = string
  default     = "app-config"
}

variable "app_hostname" {
  description = "Debe coincidir con el CNAME de infra/dns. www y el apex quedan libres para la landing."
  type        = string
  default     = "app.fixcomap.com"
}

variable "enable_domain_mapping" {
  description = <<-EOT
    El domain mapping falla si gh-deployer no es propietario verificado del dominio
    en Search Console (paso manual, ver README). Hasta entonces, false.
  EOT
  type        = bool
  default     = false
}
