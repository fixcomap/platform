output "static_pages_subdomains" {
  description = "Subdominio *.pages.dev de cada sitio del catálogo; destino de sus CNAME."
  value       = { for k, p in cloudflare_pages_project.static : k => p.subdomain }
}

output "webapp_hostnames" {
  value = module.catalog.webapp_hostnames
}
