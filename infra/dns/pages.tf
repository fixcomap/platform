# Landing estática en Cloudflare Pages (free tier: 500 builds/mes, ancho de banda
# ilimitado). Fuente: este repo vía la GitHub App de Cloudflare, sin credenciales
# en Actions: Cloudflare hace pull y publica en cada push a main. Sin build:
# sirve web/ tal cual.
resource "cloudflare_pages_project" "landing" {
  account_id        = var.cloudflare_account_id
  name              = "fixcomap-landing"
  production_branch = "main"

  source = {
    type = "github"
    config = {
      owner             = "fixcomap"
      repo_name         = "platform"
      production_branch = "main"
      # Solo develop genera previews (fixcomap-landing-<hash>.pages.dev); las
      # ramas feature/* y las de Renovate no, para no gastar builds.
      preview_deployment_setting     = "custom"
      preview_branch_includes        = ["develop"]
      preview_branch_excludes        = []
      production_deployments_enabled = true
      pr_comments_enabled            = false
      # Solo cambios en web/ disparan despliegues.
      path_includes = ["web/*"]
      path_excludes = []
    }
  }

  build_config = {
    build_command   = ""
    destination_dir = "web"
    root_dir        = ""
    build_caching   = false
  }
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
