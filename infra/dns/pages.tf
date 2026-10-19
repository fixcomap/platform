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

# Proyectos que ya existen en la cuenta. El de un sitio nuevo lo crea wrangler
# en su primer deploy, que llega después de la release que lo da de alta en el
# catálogo: hasta entonces se omite y entra (import, dominios, CNAME) en el
# siguiente apply de dns.
data "cloudflare_pages_projects" "all" {
  account_id = var.cloudflare_account_id
  max_items  = 100

  lifecycle {
    # Un listado vacío por un fallo de la API dejaría fuera todos los sitios y
    # el plan querría borrar sus dominios: mejor fallar.
    postcondition {
      condition     = length(self.result) > 0
      error_message = "cloudflare_pages_projects vacío: no se planifica dns sin la lista de proyectos."
    }
  }
}

locals {
  existing_pages = toset([for p in data.cloudflare_pages_projects.all.result : p.name])

  # Sitios static y fronts de webapps; catalog-check (pr-checks) garantiza que
  # proyectos y hostnames no se repiten.
  pages_sites = { for k, name in module.catalog.pages_sites : k => name if contains(local.existing_pages, name) }
  pages_hostnames = {
    for h, x in module.catalog.pages_hostnames : h => x if contains(keys(local.pages_sites), x.site)
  }
}

resource "cloudflare_pages_project" "static" {
  for_each          = local.pages_sites
  account_id        = var.cloudflare_account_id
  name              = each.value
  production_branch = "main"

  # Segunda red: si un sitio desaparece del listado o del catálogo por error, el
  # plan falla en vez de borrar el proyecto (y con él sus dominios y CNAME).
  # Dar de baja un sitio es quitar esto a propósito en la misma PR.
  lifecycle {
    prevent_destroy = true
  }
}

# El proyecto lo crea wrangler en el primer deploy y aquí se importa al estado;
# si ya está en el estado, el bloque se ignora. Crearlo desde el provider con
# source = github exige la GitHub App de Cloudflare (error 8000011), que no
# queremos: publica el pipeline, no Cloudflare. Consecuencia: el primer deploy
# de un sitio nuevo debe ir antes de la release de platform que lo importa.
import {
  for_each = local.pages_sites
  to       = cloudflare_pages_project.static[each.key]
  id       = "${var.cloudflare_account_id}/${each.value}"
}

# Pages emite el certificado (Google Trust Services) al ver el CNAME.
resource "cloudflare_pages_domain" "static" {
  for_each     = local.pages_hostnames
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.static[each.value.site].name
  name         = each.key
}

# Proxied (naranja): obligatorio para Pages con dominio propio. En el apex
# Cloudflare aplana el CNAME automáticamente. Un hostname de rama (front de
# desarrollo de una webapp) apunta al alias <rama>.<proyecto>.pages.dev.
resource "cloudflare_dns_record" "static" {
  for_each = local.pages_hostnames
  zone_id  = var.cloudflare_zone_id
  name     = each.key
  type     = "CNAME"
  content = (each.value.branch == "" ? cloudflare_pages_project.static[each.value.site].subdomain
  : "${each.value.branch}.${cloudflare_pages_project.static[each.value.site].subdomain}")
  ttl     = 1
  proxied = true
  comment = "catalog/${each.value.site}.yaml en Cloudflare Pages (OpenTofu, infra/dns)"
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
