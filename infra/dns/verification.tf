# TXT de verificación de fixcomap.com en Google (Search Console / Site
# Verification API); lo exigen los domain mappings de Cloud Run. Se creó a mano
# el 16/09/2026; pasa a código para que drift.yml lo vigile. El registro manual
# se borra en Cloudflare justo antes del apply (Cloudflare rechaza dos TXT
# idénticos). El valor no es secreto: es público en DNS por definición.
resource "cloudflare_dns_record" "google_site_verification" {
  zone_id = var.cloudflare_zone_id
  name    = "fixcomap.com"
  type    = "TXT"
  content = "\"google-site-verification=_QHWkyR_o7qKzd2YwEzXqEMaJVsNYN0uaD9xan9N1KQ\""
  ttl     = 1
  proxied = false
  comment = "Verificación de dominio en Google (OpenTofu, infra/dns)"
}
