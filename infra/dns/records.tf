# CNAME de cada hostname de webapp del catálogo hacia el frontal de domain
# mappings de Cloud Run. Es estático: no depende del servicio, así que existe
# antes que el domain mapping (mientras tanto Google responde 404) y debe ir en
# una release anterior a él: si van juntos, el mapping se crea antes que el DNS,
# el NXDOMAIN queda cacheado 30 min (SOA min 1800) y el certificado tarda ~1 h.
# Sin proxy naranja: Google necesita resolver el CNAME real para emitir el
# certificado gestionado. Un nivel más de subdominio (plantilla.dev) no es
# problema: el certificado lo emite Google, no Universal SSL.
resource "cloudflare_dns_record" "webapp" {
  for_each = module.catalog.webapp_hostnames

  zone_id = var.cloudflare_zone_id
  name    = each.key
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 300
  proxied = false
  comment = "Cloud Run ${each.value} (catalog, OpenTofu, infra/dns)"
}

# Registros anteriores al catálogo (servicios app y app-dev).
moved {
  from = cloudflare_dns_record.app["app.fixcomap.com"]
  to   = cloudflare_dns_record.webapp["app.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.app["plantilla.fixcomap.com"]
  to   = cloudflare_dns_record.webapp["plantilla.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.app_dev["dev.fixcomap.com"]
  to   = cloudflare_dns_record.webapp["dev.fixcomap.com"]
}
moved {
  from = cloudflare_dns_record.app_dev["plantilla.dev.fixcomap.com"]
  to   = cloudflare_dns_record.webapp["plantilla.dev.fixcomap.com"]
}
