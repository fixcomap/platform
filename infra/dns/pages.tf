# Sitios estáticos en Cloudflare Pages (free tier: 500 despliegues/mes, ancho de
# banda ilimitado), uno por cada catalog/<repo>.yaml con type: static. Proyectos
# de "direct upload": publica el pipeline estándar (cicd.yml, wrangler), así el
# despliegue pasa por sus checks y queda registrado en Actions. Aquí solo el
# proyecto, sus dominios y el DNS: dar de alta un sitio es añadir su fichero al
# catálogo.
module "catalog" {
  source = "../../modules/catalog"
  dir    = "${path.module}/../../catalog"
}

locals {
  static_sites = module.catalog.static
  # catalog-check (pr-checks) garantiza que los hostnames no se repiten.
  static_hostnames = module.catalog.static_hostnames
}

resource "cloudflare_pages_project" "static" {
  for_each          = local.static_sites
  account_id        = var.cloudflare_account_id
  name              = each.value.pages_project
  production_branch = "main"
}

# El proyecto lo crea wrangler en el primer deploy y aquí se importa al estado;
# si ya está en el estado, el bloque se ignora. Crearlo desde el provider con
# source = github exige la GitHub App de Cloudflare (error 8000011), que no
# queremos: publica el pipeline, no Cloudflare. Consecuencia: el primer deploy
# de un sitio nuevo debe ir antes de la release de platform que lo importa.
import {
  for_each = local.static_sites
  to       = cloudflare_pages_project.static[each.key]
  id       = "${var.cloudflare_account_id}/${each.value.pages_project}"
}

# Pages emite el certificado (Google Trust Services) al ver el CNAME.
resource "cloudflare_pages_domain" "static" {
  for_each     = local.static_hostnames
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.static[each.value].name
  name         = each.key
}

# Proxied (naranja): obligatorio para Pages con dominio propio. En el apex
# Cloudflare aplana el CNAME automáticamente.
resource "cloudflare_dns_record" "static" {
  for_each = local.static_hostnames
  zone_id  = var.cloudflare_zone_id
  name     = each.key
  type     = "CNAME"
  content  = cloudflare_pages_project.static[each.value].subdomain
  ttl      = 1
  proxied  = true
  comment  = "catalog/${each.value}.yaml en Cloudflare Pages (OpenTofu, infra/dns)"
}

# Recursos anteriores al catálogo, escritos a mano por sitio.
moved {
  from = cloudflare_pages_project.landing
  to   = cloudflare_pages_project.static["web"]
}
moved {
  from = cloudflare_pages_project.agustin
  to   = cloudflare_pages_project.static["agustin"]
}
moved {
  from = cloudflare_pages_project.elias
  to   = cloudflare_pages_project.static["elias"]
}
moved {
  from = cloudflare_pages_domain.apex
  to   = cloudflare_pages_domain.static["fixcomap.com"]
}
moved {
  from = cloudflare_pages_domain.www
  to   = cloudflare_pages_domain.static["www.fixcomap.com"]
}
moved {
  from = cloudflare_pages_domain.agustin
  to   = cloudflare_pages_domain.static["agustin.fixcomap.com"]
}
moved {
  from = cloudflare_pages_domain.elias
  to   = cloudflare_pages_domain.static["elias.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.apex
  to   = cloudflare_dns_record.static["fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.www
  to   = cloudflare_dns_record.static["www.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.agustin
  to   = cloudflare_dns_record.static["agustin.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.elias
  to   = cloudflare_dns_record.static["elias.fixcomap.com"]
}
