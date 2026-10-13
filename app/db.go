package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// La cadena de conexión llega como fichero (volumen de Secret Manager), no como
// variable de entorno: así una versión nueva del secreto se aplica al arrancar
// la instancia sin redesplegar. Se lee en cada comprobación por el mismo motivo.
func databaseURL() (string, error) {
	path := os.Getenv("DATABASE_URL_FILE")
	if path == "" {
		return "", errors.New("DATABASE_URL_FILE no definido")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return withDBName(strings.TrimSpace(string(b)), os.Getenv("DB_NAME"))
}

// withDBName cambia la base de datos de la cadena de conexión. Desarrollo usa
// las mismas credenciales de Neon que producción pero otra base (app_dev): sin
// secreto propio (coste 0) y con las tablas separadas de verdad, cosa que un
// search_path no garantiza detrás del pooler de Neon.
func withDBName(dsn, name string) (string, error) {
	if name == "" {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", errors.New("DB_NAME requiere una cadena de conexión en formato URL")
	}
	u.Path = "/" + name
	return u.String(), nil
}

// dbcheck abre una conexión y ejecuta SELECT 1. Nunca escribe la cadena de
// conexión en la respuesta ni en el log; los errores del driver no la incluyen.
func dbcheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	dsn, err := databaseURL()
	if err != nil {
		logger(r.Context()).Warn("dbcheck: sin cadena de conexión", "err", err)
		http.Error(w, "db not configured", http.StatusServiceUnavailable)
		return
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger(r.Context()).Error("dbcheck: open", "err", err)
		http.Error(w, "db error", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger(r.Context()).Error("dbcheck: close", "err", err)
		}
	}()

	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		logger(r.Context()).Error("dbcheck: query", "err", err)
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	if _, err := w.Write([]byte("db ok\n")); err != nil {
		logger(r.Context()).Error("write response", "err", err)
	}
}
