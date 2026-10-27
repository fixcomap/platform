# Disponibilidad de los hostnames públicos. Free tier de Cloud Monitoring: 1M
# ejecuciones de uptime check al mes (cada sonda: ~26k, cada 5 min desde 3
# regiones), políticas de alerta y notificaciones por email sin coste.
#
# Una sonda por webapp con monitoring: true y producción public, contra su
# health_path por el primer hostname: valida a la vez DNS, el certificado
# gestionado, el domain mapping y el servicio. Además la landing, que no vive
# en GCP (Cloudflare Pages) pero avisa al mismo buzón.
locals {
  uptime_checks = merge(
    {
      for s, e in local.webapp_services : s => {
        host = e.hostnames[0]
        path = e.health_path
        doc  = "Revisar Cloud Run (servicio `${s}`, ${var.region}), el domain mapping y el certificado."
      } if e.environment == "production" && e.public && local.webapps[e.webapp].monitoring
    },
    {
      landing = {
        host = "fixcomap.com"
        path = "/"
        doc  = "Revisar el último deploy de fixcomap/web (Actions) y el proyecto en Cloudflare Pages; rollback: `wrangler pages deployment list` + `rollback`."
      }
    },
  )
}

# Recursos creados por infra/app (servicio app); allí quedan como removed.
import {
  to = google_monitoring_notification_channel.email
  id = "projects/fixcomap-core/notificationChannels/10897718711476578087"
}
import {
  to = google_monitoring_uptime_check_config.site["plantilla"]
  id = "projects/fixcomap-core/uptimeCheckConfigs/plantilla-fixcomap-com-health-BAGZmrMp0QY"
}
import {
  to = google_monitoring_uptime_check_config.site["landing"]
  id = "projects/fixcomap-core/uptimeCheckConfigs/fixcomap-com-0DB8VLjCrHc"
}
import {
  to = google_monitoring_alert_policy.site_down["plantilla"]
  id = "projects/fixcomap-core/alertPolicies/3726116251374623470"
}
import {
  to = google_monitoring_alert_policy.site_down["landing"]
  id = "projects/fixcomap-core/alertPolicies/11521708875183597489"
}

resource "google_monitoring_uptime_check_config" "site" {
  for_each = local.uptime_checks

  # Cambiar el host recrea el check, y la API no deja borrarlo mientras una
  # política de alerta lo referencia: primero el nuevo, se repunta la alerta
  # y después se borra el viejo.
  lifecycle {
    create_before_destroy = true
  }

  display_name = "${each.value.host} ${each.value.path}"
  timeout      = "10s"
  period       = "300s"

  # 3 regiones es el mínimo que exige la API; una sola caída regional no alerta.
  selected_regions = ["EUROPE", "USA", "SOUTH_AMERICA"]
  checker_type     = "STATIC_IP_CHECKERS"

  http_check {
    path           = each.value.path
    port           = 443
    use_ssl        = true
    validate_ssl   = true
    request_method = "GET"
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = each.value.host
    }
  }
}

resource "google_monitoring_notification_channel" "email" {
  display_name = "Alertas de plataforma (email)"
  type         = "email"

  labels = {
    email_address = var.alert_email
  }
}

# Alerta si la sonda falla desde más de una región durante 5 minutos. Se cierra
# sola 30 min después de recuperarse.
resource "google_monitoring_alert_policy" "site_down" {
  for_each = local.uptime_checks

  display_name = "${each.value.host} caída"
  combiner     = "OR"

  conditions {
    display_name = "uptime check fallido desde >1 región"

    condition_threshold {
      filter          = "metric.type=\"monitoring.googleapis.com/uptime_check/check_passed\" AND resource.type=\"uptime_url\" AND metric.label.check_id=\"${google_monitoring_uptime_check_config.site[each.key].uptime_check_id}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 1
      duration        = "300s"

      aggregations {
        alignment_period     = "1200s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
        group_by_fields      = ["resource.label.*"]
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  alert_strategy {
    auto_close = "1800s"
  }

  documentation {
    content   = "El uptime check de https://${each.value.host}${each.value.path} falla desde más de una región. ${each.value.doc}"
    mime_type = "text/markdown"
  }
}
