# CNAME hacia el frontal de domain mappings de Cloud Run. Es estático: no depende
# de nada de L2, así que puede existir antes que el domain mapping (mientras
# tanto Google responde 404). Sin proxy naranja: Google necesita resolver el
# CNAME real para emitir el certificado gestionado.
moved {
  from = cloudflare_dns_record.app
  to   = cloudflare_dns_record.app["app.fixcomap.com"]
}

resource "cloudflare_dns_record" "app" {
  for_each = var.app_hostnames

  zone_id = var.cloudflare_zone_id
  name    = each.value
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 300
  proxied = false
  comment = "Cloud Run domain mapping (OpenTofu, infra/dns)"
}

moved {
  from = cloudflare_dns_record.app_dev
  to   = cloudflare_dns_record.app_dev["dev.fixcomap.com"]
}

# Un nivel más de subdominio (plantilla.dev) no es problema: sin proxy de
# Cloudflare, el certificado lo emite Google y no depende de Universal SSL.
resource "cloudflare_dns_record" "app_dev" {
  for_each = var.dev_hostnames

  zone_id = var.cloudflare_zone_id
  name    = each.value
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 300
  proxied = false
  comment = "Cloud Run domain mapping de desarrollo (OpenTofu, infra/dns)"
}
