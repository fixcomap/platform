variable "cloudflare_zone_id" {
  description = "Zona fixcomap.com. Llega por TF_VAR_cloudflare_zone_id (variable del environment platform)."
  type        = string
}

variable "dev_hostnames" {
  description = "Hostnames de desarrollo (servicio app-dev); el principal lo fija deploy-dev.yml."
  type        = set(string)
  default     = ["dev.fixcomap.com", "plantilla.dev.fixcomap.com"]
}

variable "app_hostnames" {
  description = "Hostnames de producción (servicio app). www y el apex quedan libres para la landing."
  type        = set(string)
  default     = ["app.fixcomap.com", "plantilla.fixcomap.com"]
}

variable "cloudflare_account_id" {
  description = "Cuenta de Cloudflare (Pages es un recurso de cuenta, no de zona). Llega por TF_VAR_cloudflare_account_id (secret del environment platform)."
  type        = string
}
