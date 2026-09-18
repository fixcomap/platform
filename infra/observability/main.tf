# Dashboard, SLOs y alertas de la app como código. Los datos llegan por OTLP
# desde Cloud Run (infra/app); aquí solo se declara cómo se miran y cuándo
# avisan. Free tier de Grafana Cloud: dashboards y alertas ilimitados.

# Data sources gestionados por Grafana Cloud (nombres fijos por stack).
data "grafana_data_source" "prom" {
  name = "grafanacloud-${var.stack_slug}-prom"
}

data "grafana_data_source" "traces" {
  name = "grafanacloud-${var.stack_slug}-traces"
}

resource "grafana_folder" "fixcomap" {
  title = "fixcomap"
}

# Mismo buzón que las alertas de Cloud Monitoring: un solo sitio donde mirar.
resource "grafana_contact_point" "email" {
  name = "fixcomap-email"

  email {
    addresses = [var.alert_email]
    message   = "{{ template \"default.message\" . }}"
  }
}

locals {
  job = var.service_name

  # Métricas de otelhttp (semconv estable) tal como las nombra Mimir.
  req_count  = "http_server_request_duration_seconds_count"
  req_bucket = "http_server_request_duration_seconds_bucket"

  # Ratio de errores (5xx / total) en una ventana. `or vector(0)` evita NoData
  # cuando no hay ningún 5xx, que es el caso normal.
  error_ratio = "((sum(rate(${local.req_count}{job=\"${local.job}\", http_response_status_code=~\"5..\"}[%s])) or vector(0)) / sum(rate(${local.req_count}{job=\"${local.job}\"}[%s])))"

  # Multi-burn-rate (Google SRE workbook): rápido = 14,4x el presupuesto en 1 h;
  # lento = 6x en 6 h. Con SLO 99,5 % → 7,2 % y 3 % de errores respectivamente.
  burn_fast = 14.4 * (1 - var.slo_availability)
  burn_slow = 6 * (1 - var.slo_availability)
}

resource "grafana_dashboard" "app" {
  folder    = grafana_folder.fixcomap.uid
  overwrite = true
  config_json = templatefile("${path.module}/dashboards/app.json.tftpl", {
    uid        = "fixcomap-app-overview"
    title      = "app / overview"
    prom_uid   = data.grafana_data_source.prom.uid
    traces_uid = data.grafana_data_source.traces.uid
    job        = local.job
    req_count  = local.req_count
    req_bucket = local.req_bucket
    slo        = var.slo_availability
    p95_target = var.slo_latency_p95_seconds
  })
}

# Alertas del SLO. Consultas instantáneas + umbral; sin datos = OK (la caída
# total la cubre el uptime check de Cloud Monitoring, que no depende de la app).
resource "grafana_rule_group" "app_slo" {
  name             = "app-slo"
  folder_uid       = grafana_folder.fixcomap.uid
  interval_seconds = 60

  rule {
    name           = "app: burn rate rápido (SLO disponibilidad)"
    condition      = "C"
    for            = "5m"
    no_data_state  = "OK"
    exec_err_state = "Error"

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.prom.uid
      relative_time_range {
        from = 3600
        to   = 0
      }
      model = jsonencode({
        refId   = "A"
        instant = true
        range   = false
        expr    = format(local.error_ratio, "1h", "1h")
      })
    }
    data {
      ref_id         = "C"
      datasource_uid = "__expr__"
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({
        refId      = "C"
        type       = "threshold"
        expression = "A"
        conditions = [{ evaluator = { type = "gt", params = [local.burn_fast] } }]
      })
    }

    annotations = {
      summary     = "Ratio de 5xx en 1 h por encima de ${format("%.1f", local.burn_fast * 100)} %: el presupuesto de error de 30 días se agota en ~2 días."
      description = "Revisar trazas con error en Tempo ({resource.service.name=\"${local.job}\" && status=error}) y los logs de Cloud Run."
      runbook_url = "https://github.com/fixcomap/platform/blob/main/CONTRIBUTING.md#runbook-cuando-algo-está-en-rojo"
    }
    labels = { severity = "critical", service = local.job, slo = "availability" }

    notification_settings {
      contact_point = grafana_contact_point.email.name
    }
  }

  rule {
    name           = "app: burn rate lento (SLO disponibilidad)"
    condition      = "C"
    for            = "30m"
    no_data_state  = "OK"
    exec_err_state = "Error"

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.prom.uid
      relative_time_range {
        from = 21600
        to   = 0
      }
      model = jsonencode({
        refId   = "A"
        instant = true
        range   = false
        expr    = format(local.error_ratio, "6h", "6h")
      })
    }
    data {
      ref_id         = "C"
      datasource_uid = "__expr__"
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({
        refId      = "C"
        type       = "threshold"
        expression = "A"
        conditions = [{ evaluator = { type = "gt", params = [local.burn_slow] } }]
      })
    }

    annotations = {
      summary     = "Ratio de 5xx en 6 h por encima de ${format("%.1f", local.burn_slow * 100)} %: consumo sostenido del presupuesto de error."
      runbook_url = "https://github.com/fixcomap/platform/blob/main/CONTRIBUTING.md#runbook-cuando-algo-está-en-rojo"
    }
    labels = { severity = "warning", service = local.job, slo = "availability" }

    notification_settings {
      contact_point = grafana_contact_point.email.name
    }
  }

  rule {
    name           = "app: latencia p95 por encima del objetivo"
    condition      = "C"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Error"

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.prom.uid
      relative_time_range {
        from = 600
        to   = 0
      }
      model = jsonencode({
        refId   = "A"
        instant = true
        range   = false
        expr    = "histogram_quantile(0.95, sum by (le) (rate(${local.req_bucket}{job=\"${local.job}\"}[10m])))"
      })
    }
    data {
      ref_id         = "C"
      datasource_uid = "__expr__"
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({
        refId      = "C"
        type       = "threshold"
        expression = "A"
        conditions = [{ evaluator = { type = "gt", params = [var.slo_latency_p95_seconds] } }]
      })
    }

    annotations = {
      summary     = "p95 de latencia por encima de ${var.slo_latency_p95_seconds * 1000} ms durante 10 min."
      description = "Comparar con go_memory_used_bytes y goroutines en el dashboard; si la instancia está saturada, revisar límites de Cloud Run (infra/app)."
    }
    labels = { severity = "warning", service = local.job, slo = "latency" }

    notification_settings {
      contact_point = grafana_contact_point.email.name
    }
  }
}
