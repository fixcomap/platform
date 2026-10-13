package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNotReady(t *testing.T) {
	rec := httptest.NewRecorder()
	notReady(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDbcheckWithoutConfig(t *testing.T) {
	t.Setenv("DATABASE_URL_FILE", "")
	rec := httptest.NewRecorder()
	dbcheck(rec, httptest.NewRequest(http.MethodGet, "/dbcheck", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDbcheckUnreachable(t *testing.T) {
	f := t.TempDir() + "/database-url"
	if err := os.WriteFile(f, []byte("postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", f)
	rec := httptest.NewRecorder()
	dbcheck(rec, httptest.NewRequest(http.MethodGet, "/dbcheck", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSession(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	now := time.Now()
	v := signSession(key, "a.b@gmail.com", now.Add(time.Hour))
	if email, ok := verifySession(key, v, now); !ok || email != "a.b@gmail.com" {
		t.Fatalf("válida rechazada: %q %v", email, ok)
	}
	if _, ok := verifySession(key, v, now.Add(2*time.Hour)); ok {
		t.Fatal("caducada aceptada")
	}
	if _, ok := verifySession([]byte(strings.Repeat("x", 32)), v, now); ok {
		t.Fatal("otra clave aceptada")
	}
	forged := signSession(key, "otro@gmail.com", now.Add(time.Hour))
	tampered := forged[:strings.LastIndexByte(forged, '.')] + v[strings.LastIndexByte(v, '.'):]
	if _, ok := verifySession(key, tampered, now); ok {
		t.Fatal("firma cambiada aceptada")
	}
}

func idToken(t *testing.T, c idClaims) string {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func TestClaimsFromIDToken(t *testing.T) {
	now := time.Now()
	ok := idClaims{Iss: "https://accounts.google.com", Aud: "cid", Exp: now.Add(time.Minute).Unix(),
		Email: "a@gmail.com", EmailVerified: true}
	if _, err := claimsFromIDToken(idToken(t, ok), "cid", now); err != nil {
		t.Fatalf("válido rechazado: %v", err)
	}
	bad := map[string]func(*idClaims){
		"aud":      func(c *idClaims) { c.Aud = "otro" },
		"iss":      func(c *idClaims) { c.Iss = "https://evil" },
		"exp":      func(c *idClaims) { c.Exp = now.Add(-time.Minute).Unix() },
		"verified": func(c *idClaims) { c.EmailVerified = false },
	}
	for name, mut := range bad {
		c := ok
		mut(&c)
		if _, err := claimsFromIDToken(idToken(t, c), "cid", now); err == nil {
			t.Errorf("%s: aceptado", name)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("APP_CONFIG", "placeholder")
	if _, err := loadConfig(); err == nil {
		t.Fatal("placeholder aceptado")
	}
	t.Setenv("APP_CONFIG", `{"google_client_id":"x","google_client_secret":"y","base_url":"https://a/",
		"session_key":"`+strings.Repeat("k", 32)+`","admins":[" Yo@Gmail.com "]}`)
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !c.isAdmin("yo@gmail.com") || c.BaseURL != "https://a" {
		t.Fatalf("normalización: %+v", c)
	}
}

func TestRequireRedirects(t *testing.T) {
	a := &auth{cfg: Config{SessionKey: strings.Repeat("k", 32)}}
	h := a.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("pasó sin sesión") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t/1/players", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDefaultLayoutValid(t *testing.T) {
	if !defaultLayout.valid() {
		t.Fatal("defaultLayout inválido")
	}
	l := defaultLayout
	l.Columns = []float64{1.5}
	if l.valid() {
		t.Fatal("columna fuera de rango aceptada")
	}
}

func TestPagesParse(t *testing.T) {
	if _, err := parsePages(); err != nil {
		t.Fatal(err)
	}
}

// Integración: solo con TEST_DATABASE_URL (Postgres desechable). Recorre el
// flujo real: equipo, jugadores, pasar lista, convocatoria y plantilla.
func TestFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no definido")
	}
	f := t.TempDir() + "/database-url"
	if err := os.WriteFile(f, []byte(dsn), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", f)
	t.Setenv("DEV_USER", "coach@test.dev")
	t.Setenv("K_SERVICE", "")

	a, err := newApp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.db.Exec(`TRUNCATE teams CASCADE`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.routes(mux)

	do := func(method, path string, form url.Values, want int) *httptest.ResponseRecorder {
		t.Helper()
		var body *strings.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		} else {
			body = strings.NewReader("")
		}
		r := httptest.NewRequest(method, path, body)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}

	team := do("POST", "/teams", url.Values{"name": {"Cómpeta"}}, 303).Header().Get("Location")
	base := strings.TrimSuffix(team, "/settings")
	do("POST", base+"/players", url.Values{"first_name": {"Cristóbal"}, "nickname": {"Cristo"}, "number": {"1"}}, 303)
	do("POST", base+"/players", url.Values{"first_name": {"Álvaro"}, "number": {"2"}}, 303)
	do("POST", base+"/players", url.Values{"first_name": {""}}, 400)
	do("GET", base+"/players", nil, 200)

	ps, err := a.st.players(context.Background(), mustID(t, base), true)
	if err != nil || len(ps) != 2 {
		t.Fatalf("players: %v %d", err, len(ps))
	}

	tr := do("POST", base+"/trainings", url.Values{"day": {"2026-10-13"}}, 303).Header().Get("Location")
	do("GET", tr, nil, 200)
	do("POST", tr, url.Values{"present": {itoa(ps[0].ID)}, "notes": {"lluvia"}}, 303)
	body := do("GET", base+"/players", nil, 200).Body.String()
	if !strings.Contains(body, "100%") || !strings.Contains(body, "0%") {
		t.Fatalf("porcentajes no aparecen:\n%s", body)
	}
	do("GET", base+"/trainings", nil, 200)

	cu := do("POST", base+"/callups", url.Values{"day": {"2026-10-18"}, "rival": {"Nerja"}}, 303).Header().Get("Location")
	do("POST", cu+"/add", url.Values{"player": {itoa(ps[1].ID)}}, 303)
	do("POST", cu+"/add", url.Values{"player": {itoa(ps[0].ID)}}, 303)
	do("POST", cu+"/add", url.Values{"player": {itoa(ps[0].ID)}}, 303) // idempotente
	in, err := a.st.callupPlayers(context.Background(), mustID(t, cu))
	if err != nil || len(in) != 2 || in[0].ID != ps[1].ID {
		t.Fatalf("orden de convocados: %v %+v", err, in)
	}
	do("GET", cu, nil, 200)
	do("POST", cu+"/remove", url.Values{"player": {itoa(ps[1].ID)}}, 303)

	// Plantilla: PNG mínimo válido.
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err := a.st.saveTemplate(context.Background(), mustID(t, base), png, "image/png", mustJSON(t, defaultLayout)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(do("GET", cu, nil, 200).Body.String(), "drawCallup") {
		t.Fatal("la convocatoria no pinta la imagen con plantilla")
	}
	do("GET", base+"/settings", nil, 200)
	do("POST", base+"/template/layout", url.Values{"layout": {`{"columns":[2]}`}}, 400)
	if got := do("GET", base+"/template.img", nil, 200).Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type %q", got)
	}

	// Otro equipo sin acceso: sin admin no se ve (404).
	a.auth.devUser = ""
	a.auth.cfg = Config{SessionKey: strings.Repeat("k", 32)}
	other, err := a.st.createTeam(context.Background(), "Otro", "otro@test.dev")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/t/"+itoa(other)+"/players", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: signSession([]byte(a.auth.cfg.SessionKey), "coach@test.dev", time.Now().Add(time.Hour))})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("acceso a equipo ajeno: %d", rec.Code)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func mustID(t *testing.T, path string) int64 {
	t.Helper()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		t.Fatalf("id de %q: %v", path, err)
	}
	return id
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
