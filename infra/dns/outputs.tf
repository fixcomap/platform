output "app_hostname" {
  value = var.app_hostname
}

output "landing_pages_subdomain" {
  description = "Subdominio *.pages.dev del proyecto; destino de los CNAME del apex y www."
  value       = cloudflare_pages_project.landing.subdomain
}
