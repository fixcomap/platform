package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
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
	return strings.TrimSpace(string(b)), nil
}

// dbcheck abre una conexión y ejecuta SELECT 1. Nunca escribe la cadena de
// conexión en la respuesta ni en el log; los errores del driver no la incluyen.
func dbcheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	dsn, err := databaseURL()
	if err != nil {
		log.Printf("dbcheck: sin cadena de conexión: %v", err)
		http.Error(w, "db not configured", http.StatusServiceUnavailable)
		return
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Printf("dbcheck: open: %v", err)
		http.Error(w, "db error", http.StatusServiceUnavailable)
		return
	}
	defer db.Close()

	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		log.Printf("dbcheck: query: %v", err)
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	if _, err := w.Write([]byte("db ok\n")); err != nil {
		log.Printf("write response: %v", err)
	}
}
