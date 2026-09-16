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
