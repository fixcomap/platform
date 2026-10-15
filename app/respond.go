package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sentence: mayúscula inicial y punto final, para mostrar errores internos
// (que por convención Go van en minúscula) como frases.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[size:]
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "!") {
		s += "."
	}
	return s
}

// Los formularios se envían con fetch desde app.js (cabecera X-Requested-With:
// fetch) para que un error se muestre como aviso sin salir de la página y sin
// perder lo escrito. Sin JavaScript, el comportamiento es el clásico.

func isFetch(r *http.Request) bool { return r.Header.Get("X-Requested-With") == "fetch" }

// httpError responde un error: JSON {"error"} para fetch, texto plano si no.
func httpError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	msg = sentence(msg)
	if isFetch(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}
	http.Error(w, msg, code)
}

// redirect: para fetch devuelve {"location"} y el navegador navega (con su
// ancla #, que fetch perdería al seguir un 303).
func redirect(w http.ResponseWriter, r *http.Request, url string, code int) {
	if isFetch(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"location": url})
		return
	}
	http.Redirect(w, r, url, code)
}
