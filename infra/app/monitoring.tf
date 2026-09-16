# Disponibilidad de app.fixcomap.com. Free tier de Cloud Monitoring: 1M ejecuciones
# de uptime check al mes (aquí ~26k: cada 5 min desde 3 regiones), políticas de
# alerta y notificaciones por email sin coste.

# Sonda HTTPS contra /healthz por el hostname público: valida a la vez DNS, el
# certificado gestionado, el domain mapping y el servicio.
resource "google_monitoring_uptime_check_config" "app" {
  display_name = "app.fixcomap.com /healthz"
  timeout      = "10s"
  period       = "300s"

  # 3 regiones es el mínimo que exige la API; una sola caída regional no alerta.
  selected_regions = ["EUROPE", "USA", "SOUTH_AMERICA"]
  checker_type     = "STATIC_IP_CHECKERS"

  http_check {
    path           = "/healthz"
    port           = 443
    use_ssl        = true
    validate_ssl   = true
    request_method = "GET"
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = var.app_hostname
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
resource "google_monitoring_alert_policy" "app_down" {
  display_name = "app.fixcomap.com caída"
  combiner     = "OR"

  conditions {
    display_name = "uptime check fallido desde >1 región"

    condition_threshold {
      filter          = "metric.type=\"monitoring.googleapis.com/uptime_check/check_passed\" AND resource.type=\"uptime_url\" AND metric.label.check_id=\"${google_monitoring_uptime_check_config.app.uptime_check_id}\""
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
    content   = "El uptime check de https://${var.app_hostname}/healthz falla desde más de una región. Revisar Cloud Run (servicio `app`, europe-west1), el domain mapping y el certificado."
    mime_type = "text/markdown"
  }
}
