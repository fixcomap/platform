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

variable "github_org" {
  description = "Organización de GitHub. El provider OIDC rechaza tokens de cualquier otra."
  type        = string
  default     = "fixcomap"
}

variable "github_repo" {
  description = "Repositorio (sin org) autorizado a suplantar las SAs."
  type        = string
  default     = "platform"
}
