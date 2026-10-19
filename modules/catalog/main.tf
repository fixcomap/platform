# Lectura única de catalog/*.yaml para todas las capas (dns, platform, webapp):
# cada una deriva de aquí lo que gestiona, con los mismos defaults. Solo
# expresiones, sin recursos. El esquema lo valida el job catalog de pr-checks.
variable "dir" {
  description = "Ruta a catalog/ desde la capa que llama."
  type        = string
}

locals {
  all = {
    for f in fileset(var.dir, "*.yaml") :
    trimsuffix(f, ".yaml") => yamldecode(file("${var.dir}/${f}"))
  }

  static = { for k, v in local.all : k => v.static if v.type == "static" }

  webapps = {
    for k, v in local.all : k => {
      service     = v.webapp.service
      runtime     = try(v.webapp.runtime, "go")
      public      = try(v.webapp.public, false)
      monitoring  = try(v.webapp.monitoring, false)
      health_path = try(v.webapp.health_path, "/health")
      # Contrato original (plantilla): APP_CONFIG + DATABASE_URL_FILE, secretos
      # <service>-config y <service>-database-url compartidos por los dos entornos.
      # Con webapp.secrets, en su lugar una variable de entorno por secreto y
      # entorno: <servicio>-<variable en kebab-case>.
      legacy     = try(v.webapp.secrets, null) == null
      secrets    = try(v.webapp.secrets, [])
      env        = try(v.webapp.env, {})
      migrate    = try(v.webapp.migrate, "")
      front      = try(v.webapp.front, null)
      production = v.webapp.production
      development = v.webapp.development
    } if v.type == "webapp"
  }

  # Un servicio Cloud Run por webapp y entorno: <service> y <service>-dev.
  webapp_services = merge([
    for k, w in local.webapps : {
      for e in ["production", "development"] : (e == "production" ? w.service : "${w.service}-dev") => {
        webapp      = k
        environment = e
        hostnames   = w[e].hostnames
        db_name     = try(w[e].db_name, "")
        public      = w.public
        health_path = w.health_path
        legacy      = w.legacy
        runtime_sa  = "${w.service}-runtime"
        config      = "${w.service}-config"
        database    = "${w.service}-database-url"
        migrate     = w.migrate
        # {api_url} y {front_url}: primer hostname de la API y del front en este entorno.
        env = {
          for name, value in w.env : name => replace(replace(value,
            "{api_url}", "https://${w[e].hostnames[0]}"),
          "{front_url}", "https://${try(w[e].front_hostnames[0], w[e].hostnames[0])}")
        }
        secrets = {
          for name in w.secrets : name => "${e == "production" ? w.service : "${w.service}-dev"}-${replace(lower(name), "_", "-")}"
        }
      }
    }
  ]...)

  # Front de una webapp en Cloud Run: proyecto de Pages como un static, con la
  # rama develop en los hostnames de desarrollo.
  fronts = { for k, w in local.webapps : k => w if w.front != null }

  pages_sites = merge(
    { for k, s in local.static : k => s.pages_project },
    { for k, w in local.fronts : k => w.front.pages_project },
  )

  # hostname => sitio y rama de Pages ("" = producción, si no <rama>.<proyecto>.pages.dev).
  pages_hostnames = merge(
    merge([for k, s in local.static : { for h in s.hostnames : h => { site = k, branch = "" } }]...),
    merge([for k, w in local.fronts : { for h in w.production.front_hostnames : h => { site = k, branch = "" } }]...),
    merge([for k, w in local.fronts : { for h in w.development.front_hostnames : h => { site = k, branch = "develop" } }]...),
  )
}

output "static" {
  description = "catalog/<k>.yaml type static => bloque static."
  value       = local.static
}

output "pages_sites" {
  description = "Sitio con proyecto de Cloudflare Pages (static o front de webapp) => nombre del proyecto."
  value       = local.pages_sites
}

output "pages_hostnames" {
  description = "hostname => { site, branch } del proyecto de Pages que lo sirve."
  value       = local.pages_hostnames
}

output "webapps" {
  description = "catalog/<k>.yaml type webapp => bloque webapp con defaults."
  value       = local.webapps
}

output "webapp_services" {
  description = "Servicio Cloud Run => webapp, entorno, hostnames, variables y nombres derivados."
  value       = local.webapp_services
}

output "webapp_hostnames" {
  description = "hostname de API => servicio Cloud Run."
  value       = merge([for s, e in local.webapp_services : { for h in e.hostnames : h => s }]...)
}
