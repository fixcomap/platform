package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Telemetría por OTLP/HTTP a Grafana Cloud. Toda la configuración llega por las
// variables estándar OTEL_* (endpoint, cabeceras con el token, nombre del
// servicio, atributos de recurso): el código no sabe a qué backend habla.
// Sin OTEL_EXPORTER_OTLP_ENDPOINT (local, previews) no se exporta nada y la
// app funciona igual: la observabilidad nunca puede tirar el servicio.
func setupOTel(ctx context.Context) (shutdown func(context.Context) error, err error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		slog.Info("otel desactivado: OTEL_EXPORTER_OTLP_ENDPOINT vacío")
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(), // OTEL_SERVICE_NAME y OTEL_RESOURCE_ATTRIBUTES
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, errors.Join(err, tp.Shutdown(ctx))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(30*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Métricas del runtime de Go (GC, goroutines, memoria): baratas y útiles
	// para distinguir "la app va lenta" de "la instancia está saturada".
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, errors.Join(err, mp.Shutdown(ctx), tp.Shutdown(ctx))
	}

	slog.Info("otel activado", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	return func(ctx context.Context) error {
		return errors.Join(mp.Shutdown(ctx), tp.Shutdown(ctx))
	}, nil
}

// Logs JSON con trace_id/span_id: Cloud Logging los correlaciona con las trazas
// y en Grafana se puede saltar de un log a su traza.
func logger(ctx context.Context) *slog.Logger {
	l := slog.Default()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		l = l.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	return l
}
