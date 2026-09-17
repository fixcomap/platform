# Disponibilidad de los hostnames públicos. Free tier de Cloud Monitoring: 1M
# ejecuciones de uptime check al mes (aquí ~52k: 2 sondas cada 5 min desde 3
# regiones), políticas de alerta y notificaciones por email sin coste.
#
# La landing no vive en GCP (Cloudflare Pages), pero la sonda sí: es la única
# alerta que tenemos y llega al mismo buzón. Si un deploy de fixcomap/web la
# deja caída, se sabe sin que nadie esté mirando.

locals {
  uptime_checks = {
    # Sonda HTTPS contra /healthz por el hostname público: valida a la vez DNS,
    # el certificado gestionado, el domain mapping y el servicio.
    app = {
      host = var.app_hostname
      path = "/healthz"
      doc  = "Revisar Cloud Run (servicio `${var.service_name}`, ${var.region}), el domain mapping y el certificado."
    }
    landing = {
      host = var.landing_hostname
      path = "/"
      doc  = "Revisar el último deploy de fixcomap/web (Actions) y el proyecto en Cloudflare Pages; rollback: `wrangler pages deployment list` + `rollback`."
    }
  }
}

moved {
  from = google_monitoring_uptime_check_config.app
  to   = google_monitoring_uptime_check_config.site["app"]
}

moved {
  from = google_monitoring_alert_policy.app_down
  to   = google_monitoring_alert_policy.site_down["app"]
}

resource "google_monitoring_uptime_check_config" "site" {
  for_each = local.uptime_checks

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
