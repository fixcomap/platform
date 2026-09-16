// Servicio placeholder: existe para que el pipeline tenga algo real que construir,
// escanear, firmar y desplegar. Se sustituirá cuando haya producto.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if _, err := fmt.Fprintf(w, "fixcomap platform %s\n", os.Getenv("APP_VERSION")); err != nil {
		// La cabecera ya salió: no se puede cambiar el status; queda constancia en el log.
		log.Printf("write response: %v", err)
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	// Cloud Run inyecta PORT; el default solo sirve para ejecutar en local.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/dbcheck", dbcheck)
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
