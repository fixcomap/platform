variable "project_id" {
  description = "ID del proyecto GCP."
  type        = string
  default     = "fixcomap-core"
}

variable "region" {
  description = "Región de Artifact Registry y Cloud Run."
  type        = string
  default     = "europe-west1"
}

variable "state_bucket" {
  description = "Bucket de estado creado en L0. Aquí solo se le añaden bindings por prefijo."
  type        = string
  default     = "fixcomap-core-tfstate"
}

variable "state_bucket_app" {
  description = "Bucket de estado de L2 creado en L0. gh-deployer solo toca este."
  type        = string
  default     = "fixcomap-core-tfstate-app"
}

variable "cloud_run_service_name" {
  description = "Nombre del servicio Cloud Run creado en L2; aquí solo se le concede el invoker."
  type        = string
  default     = "app"
}

variable "app_public" {
  description = <<-EOT
    Concede run.invoker a allUsers sobre el servicio. Es IAM, por eso vive en L1 y no
    en L2. Debe ser false hasta que L2 haya creado el servicio (deploy.yml aplica L1
    antes que L2): en un arranque desde cero, primer deploy con false y un PR que lo
    ponga a true después.
  EOT
  type        = bool
  default     = true
}

variable "github_org" {
  type    = string
  default = "fixcomap"
}

variable "github_repo" {
  type    = string
  default = "platform"
}

variable "project_number" {
  description = <<-EOT
    Número del proyecto (no el ID); forma parte del nombre del pool de WIF. Es una
    variable y no un data source para que el plan offline no necesite credenciales.
    Sale de `tofu output` en L0 o de `gcloud projects describe`.
  EOT
  type        = string
}

variable "wif_pool_id" {
  description = "ID del Workload Identity Pool creado en L0."
  type        = string
  default     = "github"
}
