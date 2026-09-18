// Servicio placeholder: existe para que el pipeline tenga algo real que construir,
// escanear, firmar y desplegar. Se sustituirá cuando haya producto.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if _, err := fmt.Fprintf(w, "fixcomap platform %s\n", os.Getenv("APP_VERSION")); err != nil {
		// La cabecera ya salió: no se puede cambiar el status; queda constancia en el log.
		logger(r.Context()).Error("write response", "err", err)
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	// Logs JSON en stdout: Cloud Logging los parsea (severity, trace) sin agente.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Key = "severity" // clave que entiende Cloud Logging
			}
			return a
		},
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownOTel, err := setupOTel(ctx)
	if err != nil {
		// Sin telemetría se sirve igual: es preferible a no arrancar.
		slog.Error("otel: no se pudo iniciar, se sigue sin exportar", "err", err)
		shutdownOTel = func(context.Context) error { return nil }
	}

	// Cloud Run inyecta PORT; el default solo sirve para ejecutar en local.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/dbcheck", dbcheck)

	// otelhttp: un span por petición y las métricas http.server.* (duración,
	// tamaño) etiquetadas por ruta y código de estado. /healthz queda fuera
	// para no llenar las trazas con la sonda de Cloud Monitoring.
	srv := &http.Server{
		Addr: ":" + port,
		Handler: otelhttp.NewHandler(mux, "http",
			otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Path
			}),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			stop()
		}
	}()

	// Apagado ordenado: Cloud Run manda SIGTERM y da 10 s; se vacían las
	// peticiones en curso y los buffers de telemetría antes de salir.
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	if err := shutdownOTel(shutdownCtx); err != nil {
		slog.Error("otel shutdown", "err", err)
	}
}
