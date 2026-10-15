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

func TestWithDBName(t *testing.T) {
	got, err := withDBName("postgres://u:p@h.neon.tech/neondb?sslmode=require", "app_dev")
	if err != nil || got != "postgres://u:p@h.neon.tech/app_dev?sslmode=require" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, _ := withDBName("host=x dbname=y", ""); got != "host=x dbname=y" {
		t.Fatalf("sin DB_NAME no debe tocar la cadena: %q", got)
	}
	if _, err := withDBName("host=x dbname=y", "app_dev"); err == nil {
		t.Fatal("formato clave=valor aceptado con DB_NAME")
	}
}

func TestCanonicalHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := canonicalHost("https://plantilla.fixcomap.com", ok)
	cases := []struct {
		method, url string
		code        int
		loc         string
	}{
		{"GET", "https://plantilla.fixcomap.com/t/1", 200, ""},
		{"GET", "https://app.fixcomap.com/t/1?x=1", 301, "https://plantilla.fixcomap.com/t/1?x=1"},
		{"POST", "https://app.fixcomap.com/logout", 308, "https://plantilla.fixcomap.com/logout"},
		{"GET", "https://app.fixcomap.com/health", 200, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.url, nil))
		if rec.Code != c.code || rec.Header().Get("Location") != c.loc {
			t.Errorf("%s %s = %d %q", c.method, c.url, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestFetchResponses(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", nil)
	r.Header.Set("X-Requested-With", "fetch")
	rec := httptest.NewRecorder()
	httpError(rec, r, "dorsal no válido", 400)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"error":"Dorsal no válido."`) {
		t.Fatalf("error para fetch: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	redirect(rec, r, "/t/1/callups/2#imagen", 303)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"location":"/t/1/callups/2#imagen"`) {
		t.Fatalf("redirect para fetch: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	redirect(rec, httptest.NewRequest("POST", "/x", nil), "/y", 303)
	if rec.Code != 303 {
		t.Fatalf("sin fetch debe seguir siendo 303: %d", rec.Code)
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
	// Dorsal repetido entre activos: rechazado con el nombre de quien lo tiene.
	if rec := do("POST", base+"/players", url.Values{"first_name": {"Otro"}, "number": {"1"}}, 409); !strings.Contains(rec.Body.String(), "Cristo") {
		t.Fatalf("mensaje de dorsal repetido: %s", rec.Body.String())
	}
	tpl, _ := parsePages()
	var buf strings.Builder
	many := make([]Player, 19)
	if err := tpl["callup"].Execute(&buf, view{Team: &Team{ID: 1}, Data: map[string]any{"Callup": Callup{}, "In": many,
		"Cycle": cycleInfo{}}}); err != nil || !strings.Contains(buf.String(), "solo permite 18") {
		t.Fatalf("aviso de 18 convocados: %v", err)
	}
	do("GET", base+"/players", nil, 200)

	ps, err := a.st.players(context.Background(), mustID(t, base), true)
	if err != nil || len(ps) != 2 {
		t.Fatalf("players: %v %d", err, len(ps))
	}

	// Ciclo: partido el 10/10; entrenos el 6 (ciclo anterior), 14 y 17 (este).
	do("POST", base+"/callups", url.Values{"day": {"2026-10-10"}}, 303)
	tr := do("POST", base+"/trainings", url.Values{"day": {"2026-10-14"}}, 303).Header().Get("Location")
	do("GET", tr, nil, 200)
	do("POST", tr, url.Values{"st_" + itoa(ps[0].ID): {"presente"}, "st_" + itoa(ps[1].ID): {"lesionado"},
		"notes": {"lluvia"}}, 303)
	do("POST", tr, url.Values{"st_" + itoa(ps[0].ID): {"vino"}}, 400)
	tr2 := do("POST", base+"/trainings", url.Values{"day": {"2026-10-17"}}, 303).Header().Get("Location")
	do("POST", tr2, url.Values{"st_" + itoa(ps[0].ID): {"justificado"}}, 303) // ps[1] sin marcar → no justificada
	old := do("POST", base+"/trainings", url.Values{"day": {"2026-10-06"}}, 303).Header().Get("Location")
	do("POST", old, url.Values{"st_" + itoa(ps[0].ID): {"presente"}}, 303)

	// Racha: ps[1] faltó el 6 y el 17 (no justificada) con una lesión el 14 en
	// medio, que ni corta ni suma → 2 seguidas. ps[0] vino el 14 → 1 (el 17 J).
	if st, _, _ := a.st.attendanceAlerts(context.Background(), mustID(t, base)); st[ps[1].ID] != 2 || st[ps[0].ID] != 1 {
		t.Fatalf("rachas: %v", st)
	}
	if !strings.Contains(do("GET", base+"/players", nil, 200).Body.String(), "2 faltas seguidas") {
		t.Fatal("falta el aviso de 2 faltas seguidas")
	}

	body := do("GET", base+"/players", nil, 200).Body.String()
	// Todo cuenta como falta: ps[0] vino 2 de 3 (67%), ps[1] 0 de 3 con 1 lesión.
	if !strings.Contains(body, "66%") || !strings.Contains(body, "0%") || !strings.Contains(body, "1L") {
		t.Fatalf("porcentajes/estados no aparecen:\n%s", body)
	}
	do("GET", base+"/trainings", nil, 200)

	// Un amistoso entre medias no corta el ciclo.
	do("POST", base+"/callups", url.Values{"day": {"2026-10-15"}, "friendly": {"1"}}, 303)
	cu := do("POST", base+"/callups", url.Values{"day": {"2026-10-18"}, "rival": {"Nerja"}}, 303).Header().Get("Location")
	since, fromMatch, ts, marks, err := a.st.cycle(context.Background(), mustID(t, base), mustDay(t, "2026-10-18"), false)
	if err != nil || !fromMatch || since.Format("2006-01-02") != "2026-10-10" || len(ts) != 2 {
		t.Fatalf("ciclo: %v %v %v %d", err, fromMatch, since, len(ts))
	}
	if marks[ps[0].ID][ts[0].ID] != Presente || marks[ps[0].ID][ts[1].ID] != Justificado ||
		marks[ps[1].ID][ts[0].ID] != Lesionado || marks[ps[1].ID][ts[1].ID] != Injustificado {
		t.Fatalf("marcas del ciclo: %+v", marks)
	}
	// Selección múltiple en un solo envío; el orden enviado es el de la imagen.
	do("POST", cu+"/add", url.Values{"player": {itoa(ps[1].ID), itoa(ps[0].ID)}}, 303)
	do("POST", cu+"/add", url.Values{"player": {itoa(ps[0].ID)}}, 303) // idempotente
	do("POST", cu+"/add", url.Values{"player": {"x"}}, 400)
	in, err := a.st.callupPlayers(context.Background(), mustID(t, cu))
	if err != nil || len(in) != 2 || in[0].ID != ps[1].ID {
		t.Fatalf("orden de convocados: %v %+v", err, in)
	}
	do("GET", cu, nil, 200)

	// Partido: datos, roles obligatorios para la imagen, resultado y tarjetas.
	do("POST", cu+"/edit", url.Values{"day": {"2026-10-18"}, "rival": {"Nerja"}, "kickoff": {"25:00"}}, 400)
	do("POST", cu+"/result", url.Values{"goals_for": {"2"}}, 400)
	do("POST", cu+"/edit", url.Values{"day": {"2026-10-18"}, "rival": {"Nerja"}, "kickoff": {"11:00"},
		"meet": {"10:00"}, "home": {"0"}, "place": {"Nerja"}}, 303)
	do("POST", cu+"/result", url.Values{"goals_for": {"3"}, "goals_against": {"1"}}, 303)
	// Editar los datos no borra el resultado.
	do("POST", cu+"/edit", url.Values{"day": {"2026-10-18"}, "rival": {"Nerja"}, "kickoff": {"11:00"},
		"meet": {"10:00"}, "home": {"0"}, "place": {"Nerja"}}, 303)
	c, err := a.st.callup(context.Background(), mustID(t, base), mustID(t, cu))
	if err != nil || c.Score() != "1-3" || c.Outcome() != "G" || c.Kickoff != "11:00" {
		t.Fatalf("partido: %v %+v", err, c)
	}
	if info := callupInfo(Team{Name: "Cómpeta"}, c); len(info) != 4 || info[0] != "Nerja - Cómpeta" {
		t.Fatalf("info imagen: %q", info)
	}
	do("POST", cu+"/roster", url.Values{}, 303) // sin capitán ni porteros: no debe fallar
	// Portero: sale de la posición POR del perfil, no se marca a mano.
	do("POST", base+"/players/"+itoa(ps[1].ID), url.Values{"first_name": {"Álvaro"}, "number": {"2"}, "active": {"1"},
		"main_pos": {"POR"}}, 303)
	do("POST", cu+"/roster", url.Values{"captain": {itoa(ps[0].ID)}}, 303)
	do("POST", cu+"/roster", url.Values{"captain": {itoa(ps[0].ID)}, "with_stats": {"1"},
		"y_" + itoa(ps[0].ID): {"2"}, "r_" + itoa(ps[1].ID): {"1"},
		"g_" + itoa(ps[0].ID): {"2"}, "g_" + itoa(ps[1].ID): {"2"}}, 303)
	do("POST", cu+"/result", url.Values{"goals_for": {"3"}, "goals_against": {"1"}, "notes": {"Buen partido"}}, 303)
	// 4 goles por jugador con 3 en el marcador → aviso; goleadores de la temporada.
	if body := do("GET", cu, nil, 200).Body.String(); !strings.Contains(body, "superan los goles") || !strings.Contains(body, "Buen partido") {
		t.Fatal("falta el aviso de goles o las observaciones")
	}
	if sc, err := a.st.scorers(context.Background(), mustID(t, base), mustDay(t, "2026-10-20")); err != nil || len(sc) != 2 || sc[0].Goals != 2 {
		t.Fatalf("goleadores: %v %+v", err, sc)
	}
	in, _ = a.st.callupPlayers(context.Background(), mustID(t, cu))
	if in[0].Goalkeeper != true || in[1].Yellow != 2 || !in[0].Red {
		t.Fatalf("roles/tarjetas: %+v", in)
	}

	do("POST", cu+"/remove", url.Values{"player": {itoa(ps[1].ID)}}, 303)
	if c, _ := a.st.callup(context.Background(), mustID(t, base), mustID(t, cu)); c.Captain.Valid {
		// ps[1] no era capitán: el capitán (ps[0]) debe seguir.
	} else {
		t.Fatal("se perdió el capitán al quitar a otro jugador")
	}

	// Plantilla: PNG mínimo válido.
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err := a.st.saveTemplate(context.Background(), mustID(t, base), png, "image/png", mustJSON(t, defaultLayout)); err != nil {
		t.Fatal(err)
	}
	// Tras quitar al portero ya no hay portero: la imagen se bloquea.
	if body := do("GET", cu, nil, 200).Body.String(); strings.Contains(body, "drawCallup(") || !strings.Contains(body, "al menos a un portero") {
		t.Fatal("la imagen debería pedir capitán y portero")
	}
	do("POST", base+"/players/"+itoa(ps[0].ID), url.Values{"first_name": {"Cristóbal"}, "nickname": {"Cristo"}, "number": {"1"},
		"active": {"1"}, "main_pos": {"POR"}}, 303)
	if !strings.Contains(do("GET", cu, nil, 200).Body.String(), "drawCallup(") {
		t.Fatal("la convocatoria no pinta la imagen con plantilla y roles")
	}
	do("GET", base+"/settings", nil, 200)

	// Sanción: ps[0] vio doble amarilla el 18/10 → sancionado en el siguiente oficial.
	next := do("POST", base+"/callups", url.Values{"day": {"2026-10-25"}}, 303).Header().Get("Location")
	if body := do("GET", next, nil, 200).Body.String(); !strings.Contains(body, "Sancionado · doble amarilla") {
		t.Fatal("no aparece la sanción por doble amarilla en el siguiente partido")
	}
	// Y no se le puede convocar para ese partido.
	if rec := do("POST", next+"/add", url.Values{"player": {itoa(ps[0].ID)}}, 409); !strings.Contains(rec.Body.String(), "sancionado") {
		t.Fatalf("convocar a un sancionado: %s", rec.Body.String())
	}
	// Una amarilla simple suma al ciclo → aparece en amonestados.
	do("POST", next+"/add", url.Values{"player": {itoa(ps[1].ID)}}, 303)
	do("POST", next+"/roster", url.Values{"with_stats": {"1"}, "y_" + itoa(ps[1].ID): {"1"}}, 303)
	if body := do("GET", base+"/players/report", nil, 200).Body.String(); !strings.Contains(body, "Informe de jugadores") ||
		!strings.Contains(body, "window.print()") || strings.Contains(body, "<nav>") {
		t.Fatal("informe imprimible incompleto")
	}
	if body := do("GET", base+"/players", nil, 200).Body.String(); !strings.Contains(body, "Amonestados de la temporada") {
		t.Fatal("falta la sección de amonestados")
	}
	// Roja directa de 3 partidos en el 25/10 → sección de expulsiones con 3 pendientes.
	do("POST", next+"/roster", url.Values{"with_stats": {"1"}, "y_" + itoa(ps[1].ID): {"1"}, "r_" + itoa(ps[1].ID): {"1"}, "rg_" + itoa(ps[1].ID): {"3"}}, 303)
	if body := do("GET", base+"/players", nil, 200).Body.String(); !strings.Contains(body, "Expulsiones y sanciones") || !strings.Contains(body, "3 partidos") {
		t.Fatal("falta la sección de expulsiones con los partidos pendientes")
	}
	// Si el del 18/10 fue amistoso, las tarjetas no cuentan.
	do("POST", cu+"/edit", url.Values{"day": {"2026-10-18"}, "friendly": {"1"}}, 303)
	if body := do("GET", next, nil, 200).Body.String(); strings.Contains(body, "Sancionado") {
		t.Fatal("un amistoso no debería sancionar")
	}
	do("POST", base+"/template/layout", url.Values{"layout": {`{"columns":[2]}`}}, 400)
	if got := do("GET", base+"/template.img", nil, 200).Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type %q", got)
	}

	// Llegadas tarde: 5 de los últimos 8 → aviso; tarde cuenta como asistencia.
	for _, d := range []string{"2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06", "2026-11-07"} {
		tr := do("POST", base+"/trainings", url.Values{"day": {d}}, 303).Header().Get("Location")
		do("POST", tr, url.Values{"st_" + itoa(ps[0].ID): {"tarde"}}, 303)
	}
	streak, late, _ := a.st.attendanceAlerts(context.Background(), mustID(t, base))
	if late[ps[0].ID] != 5 || streak[ps[0].ID] != 0 {
		t.Fatalf("tardes/racha: late=%v streak=%v", late, streak)
	}
	if !strings.Contains(do("GET", base+"/players", nil, 200).Body.String(), "5 tardes en los últimos 8") {
		t.Fatal("falta el aviso de llegadas tarde")
	}

	// Perfil: solo sus datos (asistencia, partidos, goles, tarjetas).
	pr, err := a.st.profile(context.Background(), mustID(t, base), ps[0].ID, mustDay(t, "2026-11-10"))
	if err != nil || pr.Sessions == 0 || pr.ByStatus[Tarde] != 5 || len(pr.Matches) == 0 {
		t.Fatalf("perfil: %v %+v", err, pr)
	}
	if body := do("GET", base+"/players/"+itoa(ps[0].ID), nil, 200).Body.String(); !strings.Contains(body, "asistencia") ||
		!strings.Contains(body, "Editar valores") {
		t.Fatal("perfil incompleto")
	}

	// Selector de equipo: el admin ve los dos equipos en la cabecera.
	if _, err := a.st.createTeam(context.Background(), "Cadete", "coach@test.dev"); err != nil {
		t.Fatal(err)
	}
	if body := do("GET", base+"/players", nil, 200).Body.String(); !strings.Contains(body, "team-switch") || !strings.Contains(body, "Cadete") {
		t.Fatal("falta el selector de equipo con varios equipos")
	}

	// Posiciones (principal primero) y cualidades del 1 al 10 en pasos de 0,5.
	do("POST", base+"/players/"+itoa(ps[0].ID), url.Values{"first_name": {"Cristóbal"}, "nickname": {"Cristo"}, "number": {"1"},
		"active": {"1"}, "main_pos": {"MC"}, "pos": {"DFC", "MC"}}, 303)
	if p, _ := a.st.player(context.Background(), mustID(t, base), ps[0].ID); p.Positions != "MC,DFC" {
		t.Fatalf("posiciones: %q", p.Positions)
	}
	// Un solo formulario «Editar valores»: datos, posiciones y cualidades.
	do("GET", base+"/players/"+itoa(ps[0].ID)+"/edit", nil, 200)
	edit := url.Values{"first_name": {"Cristóbal"}, "nickname": {"Cristo"}, "number": {"1"}, "active": {"1"},
		"main_pos": {"MC"}, "pos": {"DFC", "MC"}, "with_skills": {"1"}, "pace": {"7,5"}, "passing": {"11"}}
	do("POST", base+"/players/"+itoa(ps[0].ID), edit, 400) // nota fuera de rango: no se guarda nada
	edit.Set("passing", "9")
	edit.Set("defending", "6")
	edit.Set("observations", "Buen compañero")
	edit.Set("improve", "Perfil izquierdo")
	if loc := do("POST", base+"/players/"+itoa(ps[0].ID), edit, 303).Header().Get("Location"); !strings.HasSuffix(loc, "/players/"+itoa(ps[0].ID)) {
		t.Fatalf("tras editar debe volver al perfil: %q", loc)
	}
	if sk, _ := a.st.skills(context.Background(), ps[0].ID); sk["pace"] != 7.5 || sk.Overall() != 7.5 || len(sk) != 3 {
		t.Fatalf("cualidades: %v overall=%v", sk, sk.Overall())
	}
	if body := do("GET", base+"/players/"+itoa(ps[0].ID), nil, 200).Body.String(); !strings.Contains(body, "Perfil izquierdo") {
		t.Fatal("el perfil no muestra las notas al admin")
	}
	if body := do("GET", base+"/players/"+itoa(ps[0].ID), nil, 200).Body.String(); !strings.Contains(body, `class="rating"`) || !strings.Contains(body, "7,5") {
		t.Fatal("el perfil no muestra las cualidades al admin")
	}

	// Planteamiento: elegir formación, colocar (solo convocados, sin repetir).
	do("POST", cu+"/lineup", url.Values{"formation": {"5-5-0"}}, 400)
	do("POST", cu+"/lineup", url.Values{"formation": {"4-3-3"}}, 303)
	do("POST", cu+"/lineup", url.Values{"formation": {"4-3-3"}, "slot_0": {itoa(ps[0].ID)}, "slot_7": {itoa(ps[0].ID)}}, 409)
	do("POST", cu+"/lineup", url.Values{"formation": {"4-3-3"}, "slot_6": {itoa(ps[0].ID)}}, 303)
	if l, _ := a.st.lineup(context.Background(), mustID(t, cu)); l[6] != ps[0].ID || len(l) != 1 {
		t.Fatalf("planteamiento: %v", l)
	}
	if body := do("GET", cu, nil, 200).Body.String(); !strings.Contains(body, "Guardar planteamiento") || !strings.Contains(body, "Convocados que juegan de MC") {
		t.Fatal("falta el planteamiento con sugerencias por posición")
	}
	// Cambiar de formación vacía los puestos.
	do("POST", cu+"/lineup", url.Values{"formation": {"3-5-2"}, "slot_6": {itoa(ps[0].ID)}}, 303)
	if l, _ := a.st.lineup(context.Background(), mustID(t, cu)); len(l) != 0 {
		t.Fatalf("al cambiar de formación deben vaciarse los puestos: %v", l)
	}

	// Pizarra: desde formación y desde el planteamiento de un partido; guardar
	// por JSON acota coordenadas y textos.
	do("GET", base+"/boards", nil, 200)
	do("POST", base+"/boards", url.Values{"name": {""}, "origin": {"f:4-4-2"}}, 400)
	bd := do("POST", base+"/boards", url.Values{"name": {"Salida"}, "origin": {"f:4-4-2"}}, 303).Header().Get("Location")
	if body := do("GET", bd, nil, 200).Body.String(); !strings.Contains(body, "initBoard") {
		t.Fatal("la pizarra no carga el editor")
	}
	b0, _ := a.st.board(context.Background(), mustID(t, base), mustID(t, bd))
	if len(b0.State.Tokens) != 11 || b0.State.Tokens[0].Label != "POR" {
		t.Fatalf("pizarra desde 4-4-2: %+v", b0.State.Tokens)
	}
	do("POST", cu+"/lineup", url.Values{"formation": {"4-3-3"}}, 303)
	do("POST", cu+"/lineup", url.Values{"formation": {"4-3-3"}, "slot_0": {itoa(ps[0].ID)}}, 303)
	bc := do("POST", base+"/boards", url.Values{"name": {"Del partido"}, "origin": {"c:" + itoa(mustID(t, cu))}}, 303).Header().Get("Location")
	if b1, _ := a.st.board(context.Background(), mustID(t, base), mustID(t, bc)); b1.State.Tokens[0].Name != "Cristo" {
		t.Fatalf("pizarra desde planteamiento: %+v", b1.State.Tokens[0])
	}
	save := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", bd, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Fatalf("guardar pizarra = %d, want %d: %s", rec.Code, want, rec.Body.String())
		}
	}
	save(`{"name":"Córner","state":{"tokens":[{"label":"9","name":"Nombre larguísimo de más de veinticuatro","x":500,"y":-3}],
		"ball":{"x":10,"y":10},"arrows":[{"from":{"x":1,"y":1},"to":{"x":30,"y":40}}]}}`, 204)
	b2, _ := a.st.board(context.Background(), mustID(t, base), mustID(t, bd))
	if b2.Name != "Córner" || b2.State.Tokens[0].X != pitchW || b2.State.Tokens[0].Y != 0 || len([]rune(b2.State.Tokens[0].Name)) != maxLabel || len(b2.State.Arrows) != 1 {
		t.Fatalf("pizarra guardada: %+v", b2)
	}
	save(`{"name":"","state":{}}`, 400)
	save(`no es json`, 400)
	do("POST", bd+"/delete", nil, 303)

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

	// Entrenador (no admin) de su propio equipo: ni ve ni cambia los ajustes.
	r = httptest.NewRequest("GET", base+"/settings", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: signSession([]byte(a.auth.cfg.SessionKey), "coach@test.dev", time.Now().Add(time.Hour))})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("entrenador viendo ajustes: %d", rec.Code)
	}
	r = httptest.NewRequest("POST", base+"/coaches", strings.NewReader("email=x%40test.dev"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: signSession([]byte(a.auth.cfg.SessionKey), "coach@test.dev", time.Now().Add(time.Hour))})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("entrenador gestionando entrenadores: %d", rec.Code)
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

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
