variable "project_id" {
  description = "ID del proyecto GCP."
  type        = string
  default     = "fixcomap-core"
}

variable "region" {
  description = "Región por defecto. europe-west1 por cercanía y por estar entre las tier-1 de Cloud Run."
  type        = string
  default     = "europe-west1"
}

variable "state_bucket" {
  description = "Bucket de estado de todas las capas. Solo se crea aquí; el resto lo consume."
  type        = string
  default     = "fixcomap-core-tfstate"
}

variable "state_bucket_app" {
  description = <<-EOT
    Bucket de estado exclusivo de L2. Separado porque gh-deployer necesita list/get/put
    sobre su estado y las condiciones IAM por prefijo no autorizan storage.objects.list;
    un bucket propio evita darle acceso al estado de L0/L1.
  EOT
  type        = string
  default     = "fixcomap-core-tfstate-app"
}

variable "github_org" {
  description = "Organización de GitHub. El provider OIDC rechaza tokens de cualquier otra."
  type        = string
  default     = "fixcomap"
}

variable "github_org_id" {
  description = <<-EOT
    ID numérico de la organización (gh api orgs/fixcomap --jq .id). Inmutable: un nombre
    de organización puede borrarse y ser registrado por otro; el ID no.
  EOT
  type        = string
  default     = "329960363"
}

variable "github_repo" {
  description = "Repositorio (sin org) autorizado a suplantar las SAs."
  type        = string
  default     = "platform"
}
