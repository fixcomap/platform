// App de asistencia a entrenos y convocatorias para entrenadores de fútbol base.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// notReady responde mientras falte configuración o base de datos: el servicio
// arranca igual (/health verde, deploy no bloqueado) pero no sirve datos.
func notReady(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "servicio sin configurar", http.StatusServiceUnavailable)
}

// newApp monta la app si hay configuración y BD; si no, devuelve el motivo.
func newApp(ctx context.Context) (*app, error) {
	a := &auth{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		tokenURL:   "https://oauth2.googleapis.com/token",
	}
	// DEV_USER solo fuera de Cloud Run (K_SERVICE lo define Cloud Run): en
	// producción es imposible saltarse el login aunque alguien lo configure.
	if dev := os.Getenv("DEV_USER"); dev != "" && os.Getenv("K_SERVICE") == "" {
		a.devUser = normEmail(dev)
	} else {
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}
		a.cfg = cfg
	}
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := openDB(dbCtx)
	if err != nil {
		return nil, err
	}
	return &app{st: store{db: db}, auth: a, pages: pages}, nil
}

func health(w http.ResponseWriter, _ *http.Request) {
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
	// /health y no /healthz: Cloud Run reserva algunas rutas acabadas en "z" y
	// /healthz la contesta su frontend con un 404 sin llegar al contenedor.
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /dbcheck", dbcheck)
	if a, err := newApp(ctx); err != nil {
		slog.Error("app sin arrancar, se sirve 503", "err", err)
		mux.HandleFunc("/", notReady)
	} else {
		defer func() { _ = a.st.db.Close() }()
		a.routes(mux)
	}

	// otelhttp: un span por petición y las métricas http.server.* (duración,
	// tamaño) etiquetadas por ruta y código de estado. /health queda fuera
	// para no llenar las trazas con la sonda de Cloud Monitoring.
	srv := &http.Server{
		Addr: ":" + port,
		// CrossOriginProtection: CSRF por Sec-Fetch-Site/Origin, sin tokens en
		// cada formulario. Junto a SameSite=Lax en la cookie de sesión.
		Handler: otelhttp.NewHandler(http.NewCrossOriginProtection().Handler(mux), "http",
			otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" }),
			// Con el patrón del mux (/t/{team}/...) y no la ruta real: los ids
			// dispararían la cardinalidad de spans y métricas.
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if r.Pattern != "" {
					return r.Pattern
				}
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
