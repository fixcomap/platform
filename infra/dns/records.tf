# CNAME hacia el frontal de domain mappings de Cloud Run. Es estático: no depende
# de nada de L2, así que puede existir antes que el domain mapping (mientras
# tanto Google responde 404). Sin proxy naranja: Google necesita resolver el
# CNAME real para emitir el certificado gestionado.
resource "cloudflare_dns_record" "app" {
  zone_id = var.cloudflare_zone_id
  name    = var.app_hostname
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 300
  proxied = false
  comment = "Cloud Run domain mapping (OpenTofu, infra/dns)"
}
