terraform {
  required_version = ">= 1.12"

  required_providers {
    grafana = {
      source  = "grafana/grafana"
      version = "~> 4.46"
    }
  }

  backend "gcs" {}
}

# Grafana Cloud no federa OIDC de GitHub: el token de una service account
# (rol Editor, solo dashboards y alertas) es la segunda credencial de larga
# duración del sistema, junto a la de Cloudflare. Vive como secret del
# environment "platform" y el provider la lee de GRAFANA_AUTH.
provider "grafana" {
  url = var.grafana_url
}
