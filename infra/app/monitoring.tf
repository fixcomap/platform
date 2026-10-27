# La disponibilidad vive en infra/platform/monitoring.tf (por catálogo); estos
# recursos pasaron allí con import y aquí solo salen del estado.
removed {
  from = google_monitoring_uptime_check_config.site
  lifecycle {
    destroy = false
  }
}

removed {
  from = google_monitoring_notification_channel.email
  lifecycle {
    destroy = false
  }
}

removed {
  from = google_monitoring_alert_policy.site_down
  lifecycle {
    destroy = false
  }
}
