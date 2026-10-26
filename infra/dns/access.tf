# Desarrollo restringido (webapp.development.restricted) y sitios privados
# (static.restricted): el front y las previews de su proyecto de Pages piden
# login (código al email) y solo dejan pasar a los emails de dev-acceso-emails. La API se protege en la app
# (ACCESO_EMAILS). Con el placeholder (sin "@") no se crea nada.
# Cambiar los emails no dispara este apply: después, workflow_dispatch de deploy.
data "google_secret_manager_secret_version" "dev_acceso" {
  count  = length(module.catalog.restricted_fronts) > 0 ? 1 : 0
  secret = "dev-acceso-emails"
}

locals {
  dev_acceso_emails = length(data.google_secret_manager_secret_version.dev_acceso) == 0 ? [] : [
    for e in split(",", data.google_secret_manager_secret_version.dev_acceso[0].secret_data) : lower(trimspace(e)) if strcontains(e, "@")
  ]
  # El secreto es sensible, pero si hay emails o no, no: for_each no admite valores sensibles.
  restricted_fronts = nonsensitive(length(local.dev_acceso_emails) > 0) ? module.catalog.restricted_fronts : {}
}

# Login por código al email (sin cuenta de Cloudflare ni de Google): el único
# método de las apps de dev.
resource "cloudflare_zero_trust_access_identity_provider" "otp" {
  count = length(local.restricted_fronts) > 0 ? 1 : 0

  account_id = var.cloudflare_account_id
  name       = "Código por email"
  type       = "onetimepin"
  config     = {}
}

resource "cloudflare_zero_trust_access_policy" "dev" {
  count = length(local.restricted_fronts) > 0 ? 1 : 0

  account_id = var.cloudflare_account_id
  name       = "dev: emails de dev-acceso-emails"
  decision   = "allow"
  include    = [for e in local.dev_acceso_emails : { email = { email = e } }]
}

resource "cloudflare_zero_trust_access_application" "dev" {
  for_each = local.restricted_fronts

  account_id       = var.cloudflare_account_id
  name             = "${each.key} (${each.value.label})"
  type             = "self_hosted"
  session_duration = "720h"
  # Solo código por email, y directo a pedirlo (sin elegir método).
  allowed_idps              = [cloudflare_zero_trust_access_identity_provider.otp[0].id]
  auto_redirect_to_identity = true
  destinations = concat(
    [for h in each.value.hostnames : { type = "public", uri = h }],
    # *.pages.dev solo con el proyecto creado: Cloudflare rechaza un dominio que no es de la cuenta.
    contains(local.existing_pages, each.value.pages_project) ? concat(
      [{ type = "public", uri = "*.${each.value.pages_project}.pages.dev" }],
      each.value.apex ? [{ type = "public", uri = "${each.value.pages_project}.pages.dev" }] : [],
    ) : [],
  )
  policies = [{ id = cloudflare_zero_trust_access_policy.dev[0].id, precedence = 1 }]
}
