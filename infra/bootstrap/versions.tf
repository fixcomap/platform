terraform {
  required_version = ">= 1.12"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.3"
    }
  }

  # Configuración parcial: bucket y prefix llegan por -backend-config.
  # En el primer apply desde Cloud Shell el bucket aún no existe, así que se
  # inicializa con -backend=false y después se migra el estado (ver README).
  backend "gcs" {}
}

provider "google" {
  project = var.project_id
  region  = var.region
}
