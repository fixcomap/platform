package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

// Hora de España: "hoy" para el entreno por defecto no puede ser el de UTC.
var madrid = func() *time.Location {
	l, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.UTC
	}
	return l
}()

var funcs = template.FuncMap{
	"date": func(t time.Time) string { return t.Format("02/01/2006") },
	"weekday": func(t time.Time) string {
		return [...]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}[t.Weekday()]
	},
	"iso": func(t time.Time) string { return t.Format("2006-01-02") },
	// Símbolo corto de cada estado: cabe en el móvil al lado del nombre.
	"symbol": func(s Status) string {
		return map[Status]string{Presente: "✓", Lesionado: "L", Justificado: "J", Injustificado: "✗"}[s]
	},
	"statusLabel": func(s Status) string {
		return map[Status]string{Presente: "Vino", Lesionado: "Lesionado", Justificado: "Justificada",
			Injustificado: "No justificada"}[s]
	},
	"statuses": func() []Status { return statuses },
}

// withCycle rellena Player.Cycle con una marca por entreno del ciclo.
func withCycle(ps []Player, ts []Training, marks map[int64]map[int64]Status) {
	for i := range ps {
		ps[i].Cycle = make([]Mark, len(ts))
		for j, t := range ts {
			ps[i].Cycle[j] = Mark{Day: t.Day, Status: marks[ps[i].ID][t.ID]}
		}
	}
}

// cycleInfo describe el ciclo para la cabecera de la lista.
type cycleInfo struct {
	Since     time.Time
	FromMatch bool
	Trainings int
}

type app struct {
	st    store
	auth  *auth
	pages map[string]*template.Template
}

func parsePages() (map[string]*template.Template, error) {
	names, err := fs.Glob(templatesFS, "web/templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, n := range names {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "web/templates/"), ".html")
		if base == "layout" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templatesFS, "web/templates/layout.html", n)
		if err != nil {
			return nil, err
		}
		pages[base] = t
	}
	return pages, nil
}

// view es lo común a todas las páginas; Data lleva lo específico.
type view struct {
	User  user
	Team  *Team
	Tab   string
	Title string
	Data  any
}

// render pinta a un buffer antes de escribir: un error a mitad de plantilla
// devuelve 500 limpio en lugar de media página.
func (a *app) render(w http.ResponseWriter, r *http.Request, page string, v view) {
	v.User = currentUser(r.Context())
	var buf bytes.Buffer
	if err := a.pages[page].Execute(&buf, v); err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (a *app) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	logger(r.Context()).Error("request", "path", r.URL.Path, "err", err)
	http.Error(w, "error interno", http.StatusInternalServerError)
}

// canonicalHost redirige cualquier otro hostname (los antiguos, *.run.app) al
// de base_url: la cookie de sesión y el callback OAuth solo valen en ese host.
// /health queda fuera para que las sondas por hostname sigan funcionando.
func canonicalHost(baseURL string, next http.Handler) http.Handler {
	u, err := url.Parse(baseURL)
	if baseURL == "" || err != nil || u.Host == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == u.Host || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		// 308 conserva método y cuerpo de un POST; un GET se queda en 301.
		code := http.StatusMovedPermanently
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			code = http.StatusPermanentRedirect
		}
		http.Redirect(w, r, baseURL+r.URL.RequestURI(), code)
	})
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// teamHandler resuelve {team} y comprueba acceso antes del handler: ningún
// handler de equipo puede olvidarse de la autorización.
func (a *app) teamHandler(h func(http.ResponseWriter, *http.Request, Team)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "team")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		u := currentUser(r.Context())
		ok, err := a.st.canAccess(r.Context(), u.Email, u.Admin, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if !ok {
			http.NotFound(w, r) // 404 y no 403: no revela qué equipos existen
			return
		}
		t, err := a.st.team(r.Context(), id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		h(w, r, t)
	})
}

func (a *app) routes(mux *http.ServeMux) {
	static, _ := fs.Sub(staticFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /login", a.loginPage)
	// Pública: Google la exige para publicar la pantalla de consentimiento.
	mux.HandleFunc("GET /privacidad", a.privacyPage)
	mux.HandleFunc("GET /auth/start", a.auth.start)
	mux.HandleFunc("GET /auth/callback", a.auth.callback)
	mux.HandleFunc("POST /logout", a.auth.logout)

	req := a.auth.require
	mux.Handle("GET /{$}", req(http.HandlerFunc(a.home)))
	mux.Handle("POST /teams", req(http.HandlerFunc(a.createTeam)))

	t := func(pattern string, h func(http.ResponseWriter, *http.Request, Team)) {
		mux.Handle(pattern, req(a.teamHandler(h)))
	}
	t("GET /t/{team}", a.teamHome)
	t("GET /t/{team}/players", a.playersPage)
	t("POST /t/{team}/players", a.savePlayer)
	t("GET /t/{team}/players/{player}", a.playerPage)
	t("POST /t/{team}/players/{player}", a.savePlayer)
	t("GET /t/{team}/trainings", a.trainingsPage)
	t("POST /t/{team}/trainings", a.createTraining)
	t("GET /t/{team}/trainings/{training}", a.trainingPage)
	t("POST /t/{team}/trainings/{training}", a.saveAttendance)
	t("POST /t/{team}/trainings/{training}/delete", a.deleteTraining)
	t("GET /t/{team}/callups", a.callupsPage)
	t("POST /t/{team}/callups", a.createCallup)
	t("GET /t/{team}/callups/{callup}", a.callupPage)
	t("POST /t/{team}/callups/{callup}/add", a.callupAdd)
	t("POST /t/{team}/callups/{callup}/remove", a.callupRemove)
	t("POST /t/{team}/callups/{callup}/edit", a.editCallup)
	t("POST /t/{team}/callups/{callup}/roles", a.callupRoles)
	t("POST /t/{team}/callups/{callup}/cards", a.callupCards)
	t("POST /t/{team}/callups/{callup}/delete", a.deleteCallup)
	// Ajustes (entrenadores, plantilla y su layout) solo para admins: un
	// entrenador usa la app pero no la configura. La imagen sí la leen todos.
	t("GET /t/{team}/settings", adminOnly(a.settingsPage))
	t("POST /t/{team}/coaches", adminOnly(a.addCoach))
	t("POST /t/{team}/coaches/remove", adminOnly(a.removeCoach))
	t("POST /t/{team}/template", adminOnly(a.uploadTemplate))
	t("POST /t/{team}/template/layout", adminOnly(a.saveLayout))
	t("GET /t/{team}/template.img", a.templateImage)
}

func adminOnly(h func(http.ResponseWriter, *http.Request, Team)) func(http.ResponseWriter, *http.Request, Team) {
	return func(w http.ResponseWriter, r *http.Request, t Team) {
		if !currentUser(r.Context()).Admin {
			http.Error(w, "solo un admin puede cambiar los ajustes", http.StatusForbidden)
			return
		}
		h(w, r, t)
	}
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "login", view{Title: "Entrar"})
}

func (a *app) privacyPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "privacy", view{Title: "Privacidad"})
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	teams, err := a.st.teams(r.Context(), u.Email, u.Admin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Lo normal es un solo equipo: directo a pasar lista.
	if len(teams) == 1 && !u.Admin {
		http.Redirect(w, r, "/t/"+strconv.FormatInt(teams[0].ID, 10), http.StatusSeeOther)
		return
	}
	a.render(w, r, "home", view{Title: "Equipos", Data: teams})
}

func (a *app) createTeam(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	if !u.Admin || name == "" {
		http.Error(w, "no permitido", http.StatusForbidden)
		return
	}
	id, err := a.st.createTeam(r.Context(), name, u.Email)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/t/"+strconv.FormatInt(id, 10)+"/settings", http.StatusSeeOther)
}

func teamURL(t Team, rest string) string {
	return "/t/" + strconv.FormatInt(t.ID, 10) + rest
}

func (a *app) teamHome(w http.ResponseWriter, r *http.Request, t Team) {
	http.Redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
}

func (a *app) playersPage(w http.ResponseWriter, r *http.Request, t Team) {
	ps, err := a.st.players(r.Context(), t.ID, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	now := time.Now().In(madrid)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	since, fromMatch, ts, marks, err := a.st.cycle(r.Context(), t.ID, today, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	withCycle(ps, ts, marks)
	a.render(w, r, "players", view{Team: &t, Tab: "players", Title: "Jugadores", Data: map[string]any{
		"Players": ps, "Cycle": cycleInfo{Since: since, FromMatch: fromMatch, Trainings: len(ts)},
	}})
}

func (a *app) playerPage(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "player")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := a.st.player(r.Context(), t.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "player", view{Team: &t, Tab: "players", Title: p.DisplayName(), Data: p})
}

func (a *app) savePlayer(w http.ResponseWriter, r *http.Request, t Team) {
	p := Player{
		FirstName: strings.TrimSpace(r.FormValue("first_name")),
		LastName:  strings.TrimSpace(r.FormValue("last_name")),
		Nickname:  strings.TrimSpace(r.FormValue("nickname")),
		Active:    r.FormValue("active") != "",
	}
	if p.FirstName == "" {
		http.Error(w, "el nombre es obligatorio", http.StatusBadRequest)
		return
	}
	if n := strings.TrimSpace(r.FormValue("number")); n != "" {
		v, err := strconv.ParseInt(n, 10, 32)
		if err != nil || v < 0 || v > 999 {
			http.Error(w, "dorsal inválido", http.StatusBadRequest)
			return
		}
		p.Number = sql.NullInt64{Int64: v, Valid: true}
	}
	if r.PathValue("player") != "" {
		id, err := pathID(r, "player")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		p.ID = id
	}
	if err := a.st.savePlayer(r.Context(), t.ID, p); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/players"), http.StatusSeeOther)
}

func (a *app) trainingsPage(w http.ResponseWriter, r *http.Request, t Team) {
	ts, err := a.st.trainings(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "trainings", view{Team: &t, Tab: "trainings", Title: "Entrenos", Data: map[string]any{
		"Trainings": ts, "Today": time.Now().In(madrid),
	}})
}

func parseDay(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

func (a *app) createTraining(w http.ResponseWriter, r *http.Request, t Team) {
	day, err := parseDay(r.FormValue("day"))
	if err != nil {
		http.Error(w, "fecha inválida", http.StatusBadRequest)
		return
	}
	id, err := a.st.createTraining(r.Context(), t.ID, day)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/trainings/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

type attendanceRow struct {
	Player Player
	Status Status
}

func (a *app) trainingPage(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "training")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tr, err := a.st.training(r.Context(), t.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ps, err := a.st.players(r.Context(), t.ID, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	att, err := a.st.attendance(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rows := make([]attendanceRow, len(ps))
	for i, p := range ps {
		// Sin marca previa se propone "no justificada": se toca solo a los que vienen.
		st, ok := att[p.ID]
		if !ok {
			st = Injustificado
		}
		rows[i] = attendanceRow{Player: p, Status: st}
	}
	a.render(w, r, "training", view{Team: &t, Tab: "trainings", Title: "Entreno " + tr.Day.Format("02/01"),
		Data: map[string]any{"Training": tr, "Rows": rows, "Saved": len(att) > 0}})
}

func (a *app) saveAttendance(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "training")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulario inválido", http.StatusBadRequest)
		return
	}
	// Un campo por jugador: st_<id>=presente|lesionado|justificado|injustificado.
	status := map[int64]Status{}
	for k, v := range r.PostForm {
		pidStr, ok := strings.CutPrefix(k, "st_")
		if !ok || len(v) == 0 {
			continue
		}
		pid, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil || !validStatus(Status(v[0])) {
			http.Error(w, "estado inválido", http.StatusBadRequest)
			return
		}
		status[pid] = Status(v[0])
	}
	if err := a.st.saveAttendance(r.Context(), t.ID, id, strings.TrimSpace(r.PostFormValue("notes")), status); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
}

func (a *app) deleteTraining(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "training")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.st.deleteTraining(r.Context(), t.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
}

func (a *app) callupsPage(w http.ResponseWriter, r *http.Request, t Team) {
	cs, err := a.st.callups(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "callups", view{Team: &t, Tab: "callups", Title: "Convocatorias", Data: map[string]any{
		"Callups": cs, "Empty": Callup{Day: time.Now().In(madrid)},
	}})
}

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// callupForm lee los datos del partido comunes a crear y editar.
func callupForm(r *http.Request) (Callup, error) {
	day, err := parseDay(r.FormValue("day"))
	if err != nil {
		return Callup{}, errors.New("fecha inválida")
	}
	c := Callup{Day: day, Rival: strings.TrimSpace(r.FormValue("rival")), Place: strings.TrimSpace(r.FormValue("place"))}
	for _, f := range []struct {
		dst  *string
		name string
	}{{&c.Kickoff, "kickoff"}, {&c.Meet, "meet"}} {
		v := strings.TrimSpace(r.FormValue(f.name))
		if v != "" && !hhmm.MatchString(v) {
			return Callup{}, errors.New("hora inválida")
		}
		*f.dst = v
	}
	switch r.FormValue("home") {
	case "1":
		c.Home = sql.NullBool{Bool: true, Valid: true}
	case "0":
		c.Home = sql.NullBool{Valid: true}
	}
	return c, nil
}

func (a *app) createCallup(w http.ResponseWriter, r *http.Request, t Team) {
	c, err := callupForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := a.st.createCallup(r.Context(), t.ID, c)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

func (a *app) callupPage(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "callup")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.st.callup(r.Context(), t.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	in, err := a.st.callupPlayers(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	all, err := a.st.players(r.Context(), t.ID, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	called := map[int64]bool{}
	names := make([]string, len(in))
	goalkeepers := 0
	for i, p := range in {
		called[p.ID] = true
		names[i] = p.DisplayName()
		if c.Captain.Valid && c.Captain.Int64 == p.ID {
			names[i] += " (C)"
		}
		if p.Goalkeeper {
			names[i] += " (P)"
			goalkeepers++
		}
	}
	// Asistencia del ciclo previo a este partido: es lo que se mira al convocar.
	since, fromMatch, ts, marks, err := a.st.cycle(r.Context(), t.ID, c.Day, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	withCycle(all, ts, marks)
	withCycle(in, ts, marks)
	var available []Player
	for _, p := range all {
		if !called[p.ID] {
			available = append(available, p)
		}
	}
	// Los nombres y el layout van al canvas: html/template los serializa como
	// JSON escapado al usarlos dentro de <script>.
	data := map[string]any{"Callup": c, "In": in, "Available": available,
		"Cycle": cycleInfo{Since: since, FromMatch: fromMatch, Trainings: len(ts)},
		// La imagen solo se genera con capitán y al menos un portero marcados.
		"RolesReady": c.Captain.Valid && goalkeepers > 0, "Info": callupInfo(t, c)}
	if l, at, err := a.layout(r, t.ID); err == nil {
		data["Layout"] = l
		data["TemplateVersion"] = at.Unix()
		data["Names"] = names
	} else if !errors.Is(err, sql.ErrNoRows) {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "callup", view{Team: &t, Tab: "callups", Title: "Convocatoria " + c.Day.Format("02/01"), Data: data})
}

// callupInfo son las líneas con los datos del partido que van en la imagen.
func callupInfo(t Team, c Callup) []string {
	var out []string
	if c.Rival != "" {
		switch {
		case c.Home.Valid && c.Home.Bool:
			out = append(out, t.Name+" - "+c.Rival)
		case c.Home.Valid:
			out = append(out, c.Rival+" - "+t.Name)
		default:
			out = append(out, "vs "+c.Rival)
		}
	}
	day := funcs["weekday"].(func(time.Time) string)(c.Day) + " " + c.Day.Format("02/01")
	if c.Kickoff != "" {
		day += " · " + c.Kickoff
	}
	out = append(out, day)
	if c.Place != "" {
		out = append(out, c.Place)
	}
	if c.Meet != "" {
		out = append(out, "Citación "+c.Meet)
	}
	return out
}

func (a *app) callupID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathID(r, "callup")
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

func (a *app) editCallup(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	c, err := callupForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.ID = id
	// Resultado: los dos goles o ninguno (partido sin jugar).
	gf, ga := strings.TrimSpace(r.FormValue("goals_for")), strings.TrimSpace(r.FormValue("goals_against"))
	if (gf == "") != (ga == "") {
		http.Error(w, "resultado incompleto: faltan los goles de uno de los dos", http.StatusBadRequest)
		return
	}
	if gf != "" {
		f, err1 := strconv.ParseInt(gf, 10, 32)
		g, err2 := strconv.ParseInt(ga, 10, 32)
		if err1 != nil || err2 != nil || f < 0 || g < 0 || f > 99 || g > 99 {
			http.Error(w, "resultado inválido", http.StatusBadRequest)
			return
		}
		c.GoalsFor = sql.NullInt64{Int64: f, Valid: true}
		c.GoalsAg = sql.NullInt64{Int64: g, Valid: true}
	}
	if err := a.st.updateCallup(r.Context(), t.ID, c); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

func (a *app) callupRoles(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulario inválido", http.StatusBadRequest)
		return
	}
	captain, _ := strconv.ParseInt(r.PostFormValue("captain"), 10, 64)
	var gks []int64
	for _, v := range r.PostForm["goalkeeper"] {
		if pid, err := strconv.ParseInt(v, 10, 64); err == nil {
			gks = append(gks, pid)
		}
	}
	if err := a.st.setRoles(r.Context(), t.ID, id, captain, gks); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)+"#imagen"), http.StatusSeeOther)
}

func (a *app) callupCards(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	in, err := a.st.callupPlayers(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for _, p := range in {
		pid := strconv.FormatInt(p.ID, 10)
		y, err := strconv.Atoi(r.FormValue("y_" + pid))
		if err != nil || y < 0 || y > 2 {
			y = 0
		}
		if err := a.st.setCards(r.Context(), t.ID, id, p.ID, y, r.FormValue("r_"+pid) != ""); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)+"#partido"), http.StatusSeeOther)
}

func (a *app) callupAction(w http.ResponseWriter, r *http.Request, t Team,
	f func(teamID, callupID, playerID int64) error) {
	cid, err := pathID(r, "callup")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pid, err := strconv.ParseInt(r.FormValue("player"), 10, 64)
	if err != nil {
		http.Error(w, "jugador inválido", http.StatusBadRequest)
		return
	}
	if err := f(t.ID, cid, pid); err != nil {
		a.fail(w, r, err)
		return
	}
	// El ancla deja la página donde estaba tras añadir: se convoca a varios seguidos.
	http.Redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(cid, 10)+"#lista"), http.StatusSeeOther)
}

func (a *app) callupAdd(w http.ResponseWriter, r *http.Request, t Team) {
	a.callupAction(w, r, t, func(tid, cid, pid int64) error { return a.st.addToCallup(r.Context(), tid, cid, pid) })
}

func (a *app) callupRemove(w http.ResponseWriter, r *http.Request, t Team) {
	a.callupAction(w, r, t, func(tid, cid, pid int64) error { return a.st.removeFromCallup(r.Context(), tid, cid, pid) })
}

func (a *app) deleteCallup(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "callup")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.st.deleteCallup(r.Context(), t.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/callups"), http.StatusSeeOther)
}

func (a *app) settingsPage(w http.ResponseWriter, r *http.Request, t Team) {
	cs, err := a.st.coaches(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	data := map[string]any{"Coaches": cs}
	if l, at, err := a.layout(r, t.ID); err == nil {
		data["Layout"] = l
		data["TemplateVersion"] = at.Unix()
	} else if !errors.Is(err, sql.ErrNoRows) {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "settings", view{Team: &t, Tab: "settings", Title: "Ajustes", Data: data})
}

func (a *app) addCoach(w http.ResponseWriter, r *http.Request, t Team) {
	email := normEmail(r.FormValue("email"))
	if !strings.Contains(email, "@") {
		http.Error(w, "email inválido", http.StatusBadRequest)
		return
	}
	if err := a.st.addCoach(r.Context(), t.ID, email); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
}

func (a *app) removeCoach(w http.ResponseWriter, r *http.Request, t Team) {
	email := normEmail(r.FormValue("email"))
	// Quitarse a uno mismo dejaría el equipo huérfano o al usuario fuera sin querer.
	if email == currentUser(r.Context()).Email {
		http.Error(w, "no puedes quitarte a ti mismo", http.StatusBadRequest)
		return
	}
	if err := a.st.removeCoach(r.Context(), t.ID, email); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
}

// Layout de los nombres en coordenadas relativas a la imagen (0..1), medido
// sobre la convocatoria del Cómpeta: dos columnas de 9, letra negra en negrita.
type Layout struct {
	Columns  []float64 `json:"columns"`   // x de inicio de cada columna
	Top      float64   `json:"top"`       // y del centro de la primera fila
	Bottom   float64   `json:"bottom"`    // y máxima del centro de la última fila
	Step     float64   `json:"step"`      // separación entre filas
	FontSize float64   `json:"font_size"` // relativo al ancho
	Color    string    `json:"color"`
	Info     *InfoBox  `json:"info,omitempty"`
}

// InfoBox: bloque centrado con los datos del partido (rival, fecha y hora,
// lugar, citación). Por defecto en el hueco bajo la lista de nombres.
type InfoBox struct {
	X        float64 `json:"x"` // centro horizontal
	Top      float64 `json:"top"`
	Step     float64 `json:"step"`
	FontSize float64 `json:"font_size"`
	Color    string  `json:"color"`
}

var defaultInfo = InfoBox{X: 0.5, Top: 0.86, Step: 0.034, FontSize: 0.03, Color: "#3b2f8f"}

var defaultLayout = Layout{
	Columns: []float64{0.227, 0.528}, Top: 0.477, Bottom: 0.81, Step: 0.0413, FontSize: 0.0445, Color: "#111111",
	Info: &defaultInfo,
}

func validColor(c string) bool { return strings.HasPrefix(c, "#") && len(c) <= 9 }

func (l Layout) valid() bool {
	in := func(v float64) bool { return v > 0 && v < 1 }
	if len(l.Columns) == 0 || len(l.Columns) > 4 || !in(l.Top) || !in(l.Bottom) || l.Bottom <= l.Top ||
		!in(l.Step) || !in(l.FontSize) || !validColor(l.Color) {
		return false
	}
	if i := l.Info; i != nil && (!in(i.X) || !in(i.Top) || !in(i.Step) || !in(i.FontSize) || !validColor(i.Color)) {
		return false
	}
	for _, c := range l.Columns {
		if !in(c) {
			return false
		}
	}
	return true
}

func (a *app) layout(r *http.Request, teamID int64) (Layout, time.Time, error) {
	var l Layout
	b, at, err := a.st.templateLayout(r.Context(), teamID)
	if err != nil {
		return l, at, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, at, err
	}
	if l.Info == nil {
		info := defaultInfo
		l.Info = &info
	}
	return l, at, nil
}

const maxTemplateBytes = 5 << 20

func (a *app) uploadTemplate(w http.ResponseWriter, r *http.Request, t Team) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTemplateBytes+(64<<10))
	f, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "imagen inválida o mayor de 5 MB", http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()
	img, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "imagen inválida o mayor de 5 MB", http.StatusBadRequest)
		return
	}
	// El tipo se deduce del contenido, no de lo que diga el cliente: luego se
	// sirve con ese Content-Type.
	ct := http.DetectContentType(img)
	if ct != "image/png" && ct != "image/jpeg" && ct != "image/webp" {
		http.Error(w, "solo PNG, JPEG o WebP", http.StatusBadRequest)
		return
	}
	// Al cambiar la imagen se conserva el layout ajustado si ya existía.
	layout, _, err := a.st.templateLayout(r.Context(), t.ID)
	if errors.Is(err, sql.ErrNoRows) {
		layout, err = json.Marshal(defaultLayout)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.st.saveTemplate(r.Context(), t.ID, img, ct, layout); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
}

func (a *app) saveLayout(w http.ResponseWriter, r *http.Request, t Team) {
	var l Layout
	if err := json.Unmarshal([]byte(r.FormValue("layout")), &l); err != nil || !l.valid() {
		http.Error(w, "layout inválido", http.StatusBadRequest)
		return
	}
	b, _ := json.Marshal(l)
	if err := a.st.saveLayout(r.Context(), t.ID, b); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
}

func (a *app) templateImage(w http.ResponseWriter, r *http.Request, t Team) {
	tpl, err := a.st.template(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", tpl.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// La URL lleva ?v=<updated_at>: cacheable sin miedo a servir una plantilla vieja.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(tpl.Image)
}
