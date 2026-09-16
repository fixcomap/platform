terraform {
  required_version = ">= 1.12"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.25"
    }
  }

  backend "gcs" {}
}

# Sin bloque de credenciales: el provider lee CLOUDFLARE_API_TOKEN del entorno.
# Es la única credencial de larga duración del sistema (Cloudflare no federa
# identidades OIDC de GitHub). Por eso vive como secret del environment
# "platform": solo un job aprobado por una persona puede leerla, igual que
# tf-platform. Token con permiso Zone > DNS > Edit limitado a la zona fixcomap.com.
provider "cloudflare" {}
