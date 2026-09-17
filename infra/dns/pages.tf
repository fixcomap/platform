# Landing estática en Cloudflare Pages (free tier: 500 despliegues/mes, ancho de
# banda ilimitado). Proyecto de "direct upload": sin source de Git. Publica el
# pipeline de fixcomap/web (wrangler pages deploy) con un token acotado a Pages,
# así el despliegue pasa por sus checks y queda registrado en Actions, y puede
# crecer (build, firma) sin cambiar de modelo. Aquí solo el proyecto, sus
# dominios y el DNS.
resource "cloudflare_pages_project" "landing" {
  account_id        = var.cloudflare_account_id
  name              = "fixcomap-landing"
  production_branch = "main"
}

# El proyecto lo crea wrangler (deploy.yml de fixcomap/web, idempotente) y aquí
# se importa al estado; si ya está en el estado, el bloque se ignora. Motivo:
# crearlo desde el provider con source = github exige la GitHub App de Cloudflare
# (error 8000011), que no queremos: publica el pipeline, no Cloudflare.
import {
  to = cloudflare_pages_project.landing
  id = "${var.cloudflare_account_id}/fixcomap-landing"
}

# Dominios del proyecto. Pages emite el certificado (Google Trust Services) al
# ver los CNAME de abajo.
resource "cloudflare_pages_domain" "apex" {
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.landing.name
  name         = "fixcomap.com"
}

resource "cloudflare_pages_domain" "www" {
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.landing.name
  name         = "www.fixcomap.com"
}

# Proxied (naranja): obligatorio para Pages con dominio propio. En el apex
# Cloudflare aplana el CNAME automáticamente.
resource "cloudflare_dns_record" "apex" {
  zone_id = var.cloudflare_zone_id
  name    = "fixcomap.com"
  type    = "CNAME"
  content = cloudflare_pages_project.landing.subdomain
  ttl     = 1
  proxied = true
  comment = "Landing en Cloudflare Pages (OpenTofu, infra/dns)"
}

resource "cloudflare_dns_record" "www" {
  zone_id = var.cloudflare_zone_id
  name    = "www.fixcomap.com"
  type    = "CNAME"
  content = cloudflare_pages_project.landing.subdomain
  ttl     = 1
  proxied = true
  comment = "Landing en Cloudflare Pages (OpenTofu, infra/dns)"
}

# Portfolio de Agustín (repo fixcomap/agustin): mismo modelo que la landing,
# proyecto creado por wrangler desde su ci.yml e importado aquí.
resource "cloudflare_pages_project" "agustin" {
  account_id        = var.cloudflare_account_id
  name              = "fixcomap-agustin"
  production_branch = "main"
}

import {
  to = cloudflare_pages_project.agustin
  id = "${var.cloudflare_account_id}/fixcomap-agustin"
}

resource "cloudflare_pages_domain" "agustin" {
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.agustin.name
  name         = "agustin.fixcomap.com"
}

resource "cloudflare_dns_record" "agustin" {
  zone_id = var.cloudflare_zone_id
  name    = "agustin.fixcomap.com"
  type    = "CNAME"
  content = cloudflare_pages_project.agustin.subdomain
  ttl     = 1
  proxied = true
  comment = "Portfolio de Agustín en Cloudflare Pages (OpenTofu, infra/dns)"
}

# Portfolio de Elías (repo fixcomap/elias, React+Vite con build en el pipeline).
resource "cloudflare_pages_project" "elias" {
  account_id        = var.cloudflare_account_id
  name              = "fixcomap-elias"
  production_branch = "main"
}

import {
  to = cloudflare_pages_project.elias
  id = "${var.cloudflare_account_id}/fixcomap-elias"
}

resource "cloudflare_pages_domain" "elias" {
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.elias.name
  name         = "elias.fixcomap.com"
}

resource "cloudflare_dns_record" "elias" {
  zone_id = var.cloudflare_zone_id
  name    = "elias.fixcomap.com"
  type    = "CNAME"
  content = cloudflare_pages_project.elias.subdomain
  ttl     = 1
  proxied = true
  comment = "Portfolio de Elías en Cloudflare Pages (OpenTofu, infra/dns)"
}
