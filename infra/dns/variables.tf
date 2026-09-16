variable "cloudflare_zone_id" {
  description = "Zona fixcomap.com. Llega por TF_VAR_cloudflare_zone_id (variable del environment platform)."
  type        = string
}

variable "app_hostname" {
  description = "Hostname del servicio Cloud Run. www y el apex quedan libres para la landing."
  type        = string
  default     = "app.fixcomap.com"
}
