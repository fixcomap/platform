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
      service    = v.webapp.service
      runtime    = try(v.webapp.runtime, "go")
      public     = try(v.webapp.public, false)
      monitoring = try(v.webapp.monitoring, false)
      production = {
        hostnames = v.webapp.production.hostnames
        db_name   = try(v.webapp.production.db_name, "")
      }
      development = {
        hostnames = v.webapp.development.hostnames
        db_name   = try(v.webapp.development.db_name, "")
      }
    } if v.type == "webapp"
  }

  # Un servicio Cloud Run por webapp y entorno: <service> y <service>-dev.
  webapp_services = merge([
    for k, w in local.webapps : {
      (w.service) = {
        webapp      = k
        environment = "production"
        hostnames   = w.production.hostnames
        db_name     = w.production.db_name
        public      = w.public
        runtime_sa  = "${w.service}-runtime"
        config      = "${w.service}-config"
        database    = "${w.service}-database-url"
      }
      "${w.service}-dev" = {
        webapp      = k
        environment = "development"
        hostnames   = w.development.hostnames
        db_name     = w.development.db_name
        public      = w.public
        runtime_sa  = "${w.service}-runtime"
        config      = "${w.service}-config"
        database    = "${w.service}-database-url"
      }
    }
  ]...)
}

output "static" {
  description = "catalog/<k>.yaml type static => bloque static."
  value       = local.static
}

output "static_hostnames" {
  description = "hostname => sitio static."
  value       = merge([for k, s in local.static : { for h in s.hostnames : h => k }]...)
}

output "webapps" {
  description = "catalog/<k>.yaml type webapp => bloque webapp con defaults."
  value       = local.webapps
}

output "webapp_services" {
  description = "Servicio Cloud Run => webapp, entorno, hostnames y nombres derivados."
  value       = local.webapp_services
}

output "webapp_hostnames" {
  description = "hostname => servicio Cloud Run."
  value       = merge([for s, e in local.webapp_services : { for h in e.hostnames : h => s }]...)
}
