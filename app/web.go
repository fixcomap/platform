package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

// Páginas sin el layout de la app (sin menú), pensadas para imprimir.
//
//go:embed web/print/*.html
var printFS embed.FS

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
		return map[Status]string{Presente: "✓", Tarde: "T", Lesionado: "L", Justificado: "J", Injustificado: "✗"}[s]
	},
	"statusLabel": func(s Status) string {
		return map[Status]string{Presente: "Vino", Tarde: "Llegó tarde", Lesionado: "Lesionado", Justificado: "Justificada",
			Injustificado: "No justificada"}[s]
	},
	"statuses": func() []Status { return statuses },
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	},
	"lateAlert": func() int { return lateAlert },
	"mod":       func(a, b int) int { return a % b },
	"positions": func() []Position { return positions },
	"skillDefs": func() []Skill { return skills },
	"score":     fmtScore,
	"hexGuide":  hexGuide,
	"hexLevels": func() []float64 { return []float64{2, 4, 6, 8, 10} },
	"hexLabel":  hexLabel,
	"scoreOf": func(s Skills, k string) string {
		if v, ok := s[k]; ok {
			return fmtScore(v)
		}
		return ""
	},
	"add":        func(a, b int) int { return a + b },
	"lateWindow": func() int { return lateWindow },
}

// sentOff: sancionados ahora y expulsados en la temporada, sancionados primero.
func sentOff(ps []Player) []Player {
	var out []Player
	for _, p := range ps {
		if p.Active && (p.PendingGames > 0 || p.Reds > 0 || p.Doubles > 0) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PendingGames != out[j].PendingGames {
			return out[i].PendingGames > out[j].PendingGames
		}
		return out[i].Reds+out[i].Doubles > out[j].Reds+out[j].Doubles
	})
	return out
}

// booked: amonestados de la temporada, con sancionados y apercibidos primero.
func booked(ps []Player) []Player {
	var out []Player
	for _, p := range ps {
		if p.Active && (p.CycleYellows > 0 || p.Suspension != "") {
			out = append(out, p)
		}
	}
	rank := func(p Player) int {
		switch {
		case p.Suspension != "":
			return 0
		case p.CycleYellows%yellowCycle == yellowCycle-1:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].CycleYellows > out[j].CycleYellows
	})
	return out
}

// today: fecha de hoy en España, a medianoche UTC como las columnas date.
func today() time.Time {
	n := time.Now().In(madrid)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
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
	prints, err := fs.Glob(printFS, "web/print/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range prints {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "web/print/"), ".html")
		t, err := template.New(base+".html").Funcs(funcs).ParseFS(printFS, n)
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
	Teams []Team // equipos del usuario, para el selector de la cabecera
	Tab   string
	Title string
	Data  any
}

// render pinta a un buffer antes de escribir: un error a mitad de plantilla
// devuelve 500 limpio en lugar de media página.
func (a *app) render(w http.ResponseWriter, r *http.Request, page string, v view) {
	v.User = currentUser(r.Context())
	// Con más de un equipo, la cabecera permite saltar de uno a otro.
	if v.Team != nil {
		ts, err := a.st.teams(r.Context(), v.User.Email, v.User.Admin)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		v.Teams = ts
	}
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
		httpError(w, r, "No encontrado: puede que se haya borrado.", http.StatusNotFound)
		return
	}
	logger(r.Context()).Error("request", "path", r.URL.Path, "err", err)
	httpError(w, r, "Error interno. Vuelve a intentarlo en un momento.", http.StatusInternalServerError)
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
		redirect(w, r, baseURL+r.URL.RequestURI(), code)
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
	t("GET /t/{team}/players/report", a.playersReport)
	t("GET /t/{team}/players/report.pdf", a.playersReportPDF)
	t("GET /t/{team}/players/{player}", a.playerPage)
	t("POST /t/{team}/players/{player}", a.savePlayer)
	t("GET /t/{team}/players/{player}/edit", a.playerEditPage)
	// Borrar es irreversible (se pierde el historial): solo admin.
	t("POST /t/{team}/players/{player}/delete", adminOnly(a.deletePlayer))
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
	t("POST /t/{team}/callups/{callup}/roster", a.callupRoster)
	t("POST /t/{team}/callups/{callup}/result", a.callupResult)
	t("POST /t/{team}/callups/{callup}/lineup", a.callupLineup)
	t("POST /t/{team}/callups/{callup}/delete", a.deleteCallup)
	// Ajustes (entrenadores, plantilla y su layout) solo para admins: un
	// entrenador usa la app pero no la configura. La imagen sí la leen todos.
	t("GET /t/{team}/boards", a.boardsPage)
	t("POST /t/{team}/boards", a.createBoard)
	t("GET /t/{team}/boards/{board}", a.boardPage)
	t("POST /t/{team}/boards/{board}", a.saveBoard)
	t("POST /t/{team}/boards/{board}/delete", a.deleteBoard)
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
			httpError(w, r, "Solo un admin puede cambiar los ajustes.", http.StatusForbidden)
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
		redirect(w, r, "/t/"+strconv.FormatInt(teams[0].ID, 10), http.StatusSeeOther)
		return
	}
	a.render(w, r, "home", view{Title: "Equipos", Data: teams})
}

func (a *app) createTeam(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	if !u.Admin || name == "" {
		httpError(w, r, "No tienes permiso para hacer esto.", http.StatusForbidden)
		return
	}
	id, err := a.st.createTeam(r.Context(), name, u.Email)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/t/"+strconv.FormatInt(id, 10)+"/settings", http.StatusSeeOther)
}

func teamURL(t Team, rest string) string {
	return "/t/" + strconv.FormatInt(t.ID, 10) + rest
}

func (a *app) teamHome(w http.ResponseWriter, r *http.Request, t Team) {
	redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
}

// playersData: la plantilla con ciclo, disciplina y goles; la usan la
// pantalla de Jugadores y el informe imprimible.
func (a *app) playersData(r *http.Request, t Team) (map[string]any, error) {
	ps, err := a.st.players(r.Context(), t.ID, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(madrid)
	since, fromMatch, ts, marks, err := a.st.cycle(r.Context(), t.ID, today(), true)
	if err != nil {
		return nil, err
	}
	withCycle(ps, ts, marks)
	// Sanción para el próximo partido y tarjetas de la temporada.
	disc, err := a.st.discipline(r.Context(), t.ID, now)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].Suspension = disc.Next[ps[i].ID]
		ps[i].PendingGames = disc.NextGames[ps[i].ID]
		ps[i].CycleYellows = disc.Yellows[ps[i].ID]
		ps[i].Reds = disc.Reds[ps[i].ID]
		ps[i].Doubles = disc.DoubleYellows[ps[i].ID]
	}
	scorers, err := a.st.scorers(r.Context(), t.ID, now)
	if err != nil {
		return nil, err
	}
	goals := map[int64]int{}
	for _, sc := range scorers {
		goals[sc.Player.ID] = sc.Goals
	}
	for i := range ps {
		ps[i].Goals = goals[ps[i].ID]
	}
	return map[string]any{
		"Players": ps, "Cycle": cycleInfo{Since: since, FromMatch: fromMatch, Trainings: len(ts)}, "Scorers": scorers,
		"Booked": booked(ps), "Sent": sentOff(ps), "Trainings": ts, "Now": now,
	}, nil
}

func (a *app) playersPage(w http.ResponseWriter, r *http.Request, t Team) {
	data, err := a.playersData(r, t)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "players", view{Team: &t, Tab: "players", Title: "Jugadores", Data: data})
}

// playersReport: el mismo resumen en una página para imprimir o guardar como
// PDF (A4 apaisado) y mandarlo al grupo. Sin datos privados.
func (a *app) playersReport(w http.ResponseWriter, r *http.Request, t Team) {
	data, err := a.playersData(r, t)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "report", view{Team: &t, Tab: "players", Title: "Informe de jugadores", Data: data})
}

func (a *app) playerPage(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "player")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	now := time.Now().In(madrid)
	pr, err := a.st.profile(r.Context(), t.ID, id, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Avisos: los mismos que en la lista, calculados para este jugador.
	disc, err := a.st.discipline(r.Context(), t.ID, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	streak, late, err := a.st.attendanceAlerts(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	pr.Player.Suspension = disc.Next[id]
	pr.Player.PendingGames = disc.NextGames[id]
	pr.Player.CycleYellows = disc.Yellows[id]
	pr.Player.Streak = streak[id]
	pr.Player.Late = late[id]
	// Cualidades: privadas del cuerpo técnico, solo para admin.
	if currentUser(r.Context()).Admin {
		sk, err := a.st.skills(r.Context(), id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		pr.Skills = sk
		if pr.Notes, err = a.st.playerNotes(r.Context(), id); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.render(w, r, "player", view{Team: &t, Tab: "players", Title: pr.Player.DisplayName(), Data: pr})
}

// playerEditPage: todos los valores del jugador en un solo formulario (datos,
// posiciones y, para admin, cualidades).
func (a *app) playerEditPage(w http.ResponseWriter, r *http.Request, t Team) {
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
	var sk Skills
	var notes PlayerNotes
	if currentUser(r.Context()).Admin {
		if sk, err = a.st.skills(r.Context(), id); err != nil {
			a.fail(w, r, err)
			return
		}
		if notes, err = a.st.playerNotes(r.Context(), id); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.render(w, r, "player_edit", view{Team: &t, Tab: "players", Title: "Editar " + p.DisplayName(),
		Data: map[string]any{"Player": p, "Skills": sk, "Notes": notes}})
}

func (a *app) deletePlayer(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "player")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.st.deletePlayer(r.Context(), t.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/players"), http.StatusSeeOther)
}

// skillsForm lee las cualidades del formulario de edición; se validan antes
// de guardar nada para no dejar el jugador a medio actualizar.
func skillsForm(r *http.Request) (Skills, error) {
	sk := Skills{}
	for _, k := range skills {
		v, ok, err := parseScore(r.FormValue(k.Key))
		if err != nil {
			return nil, errors.New(k.Name + ": " + err.Error())
		}
		if ok {
			sk[k.Key] = v
		}
	}
	return sk, nil
}

func (a *app) savePlayer(w http.ResponseWriter, r *http.Request, t Team) {
	p := Player{
		FirstName: strings.TrimSpace(r.FormValue("first_name")),
		LastName:  strings.TrimSpace(r.FormValue("last_name")),
		Nickname:  strings.TrimSpace(r.FormValue("nickname")),
		Active:    r.FormValue("active") != "",
	}
	// Posiciones: casillas "pos"; la principal (radio "main") va primera.
	if err := parseForm(r); err != nil {
		httpError(w, r, "No se pudo leer el formulario. Vuelve a intentarlo.", http.StatusBadRequest)
		return
	}
	var pos []string
	if m := r.PostFormValue("main_pos"); m != "" {
		if !validPosition(m) {
			httpError(w, r, "Posición no válida.", http.StatusBadRequest)
			return
		}
		pos = append(pos, m)
	}
	for _, c := range r.PostForm["pos"] {
		if !validPosition(c) {
			httpError(w, r, "Posición no válida.", http.StatusBadRequest)
			return
		}
		if len(pos) == 0 || pos[0] != c {
			pos = append(pos, c)
		}
	}
	p.Positions = strings.Join(pos, ",")
	if p.FirstName == "" {
		httpError(w, r, "El nombre es obligatorio.", http.StatusBadRequest)
		return
	}
	if n := strings.TrimSpace(r.FormValue("number")); n != "" {
		v, err := strconv.ParseInt(n, 10, 32)
		if err != nil || v < 0 || v > 999 {
			httpError(w, r, "Dorsal no válido (0 a 999).", http.StatusBadRequest)
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
	if p.Number.Valid && (p.Active || p.ID == 0) {
		holder, err := a.st.numberHolder(r.Context(), t.ID, p.ID, p.Number.Int64)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if holder != "" {
			httpError(w, r, fmt.Sprintf("El dorsal %d ya lo tiene %s.", p.Number.Int64, holder), http.StatusConflict)
			return
		}
	}
	// Cualidades (solo admin, solo al editar): se validan antes de guardar.
	var sk Skills
	withSkills := p.ID != 0 && r.PostFormValue("with_skills") != "" && currentUser(r.Context()).Admin
	if withSkills {
		var err error
		if sk, err = skillsForm(r); err != nil {
			httpError(w, r, err.Error(), http.StatusBadRequest)
			return
		}
		if len(r.PostFormValue("observations")) > 4000 || len(r.PostFormValue("improve")) > 4000 {
			httpError(w, r, "notas demasiado largas (máx. 4000 caracteres)", http.StatusBadRequest)
			return
		}
	}
	if err := a.st.savePlayer(r.Context(), t.ID, p); err != nil {
		a.fail(w, r, err)
		return
	}
	if withSkills {
		if err := a.st.saveSkills(r.Context(), t.ID, p.ID, sk); err != nil {
			a.fail(w, r, err)
			return
		}
		notes := PlayerNotes{Observations: strings.TrimSpace(r.PostFormValue("observations")),
			Improve: strings.TrimSpace(r.PostFormValue("improve"))}
		if err := a.st.savePlayerNotes(r.Context(), t.ID, p.ID, notes); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if p.ID != 0 {
		redirect(w, r, teamURL(t, "/players/"+strconv.FormatInt(p.ID, 10)), http.StatusSeeOther)
		return
	}
	redirect(w, r, teamURL(t, "/players"), http.StatusSeeOther)
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
		httpError(w, r, "Fecha no válida.", http.StatusBadRequest)
		return
	}
	id, err := a.st.createTraining(r.Context(), t.ID, day)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/trainings/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
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
	if err := parseForm(r); err != nil {
		httpError(w, r, "No se pudo leer el formulario. Vuelve a intentarlo.", http.StatusBadRequest)
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
			httpError(w, r, "Estado de asistencia no válido.", http.StatusBadRequest)
			return
		}
		status[pid] = Status(v[0])
	}
	if err := a.st.saveAttendance(r.Context(), t.ID, id, strings.TrimSpace(r.PostFormValue("notes")), status); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
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
	redirect(w, r, teamURL(t, "/trainings"), http.StatusSeeOther)
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
		return Callup{}, errors.New("fecha no válida")
	}
	c := Callup{Day: day, Rival: strings.TrimSpace(r.FormValue("rival")), Place: strings.TrimSpace(r.FormValue("place"))}
	for _, f := range []struct {
		dst  *string
		name string
	}{{&c.Kickoff, "kickoff"}, {&c.Meet, "meet"}} {
		v := strings.TrimSpace(r.FormValue(f.name))
		if v != "" && !hhmm.MatchString(v) {
			return Callup{}, errors.New("hora no válida (formato HH:MM)")
		}
		*f.dst = v
	}
	c.Friendly = r.FormValue("friendly") != ""
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
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := a.st.createCallup(r.Context(), t.ID, c)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
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
	disc, err := a.st.discipline(r.Context(), t.ID, time.Now().In(madrid))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	suspendedIn, scored := 0, 0
	for _, p := range in {
		scored += p.Goals
	}
	for _, ps := range [][]Player{all, in} {
		for i := range ps {
			ps[i].Suspension = disc.Suspended[c.ID][ps[i].ID]
			ps[i].CycleYellows = disc.YellowsBefore[c.ID][ps[i].ID]
		}
	}
	for _, p := range in {
		if p.Suspension != "" {
			suspendedIn++
		}
	}
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
		"RolesReady": c.Captain.Valid && goalkeepers > 0, "HasCaptain": c.Captain.Valid, "HasGK": goalkeepers > 0,
		"Info":        callupInfo(t, c),
		"SuspendedIn": suspendedIn, "Formations": formations,
		// Goles y tarjetas en la lista solo desde el día del partido.
		"StatsOpen": !c.Day.After(today()),
		// Goles por jugador que superan el marcador: casi siempre un error al cargar.
		"GoalsMismatch": c.GoalsFor.Valid && int64(scored) > c.GoalsFor.Int64, "Scored": scored}
	if l, at, err := a.layout(r, t.ID); err == nil {
		data["Layout"] = l
		data["TemplateVersion"] = at.Unix()
		data["Names"] = names
	} else if !errors.Is(err, sql.ErrNoRows) {
		a.fail(w, r, err)
		return
	}
	if f, ok := formationByName(c.Formation); ok {
		assigned, err := a.st.lineup(r.Context(), c.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		slots, bench := buildLineup(f, in, assigned)
		data["Lineup"], data["Bench"] = slots, bench
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
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	c.ID = id
	if err := a.st.updateCallup(r.Context(), t.ID, c); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

// callupRoster guarda la lista de convocados de una vez: capitán y, desde el
// día del partido (with_stats), goles y tarjetas. Los porteros salen solos
// de la posición POR del perfil.
func (a *app) callupRoster(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		httpError(w, r, "No se pudo leer el formulario. Vuelve a intentarlo.", http.StatusBadRequest)
		return
	}
	captain, _ := strconv.ParseInt(r.PostFormValue("captain"), 10, 64)
	if err := a.st.setCaptain(r.Context(), t.ID, id, captain); err != nil {
		a.fail(w, r, err)
		return
	}
	if r.PostFormValue("with_stats") != "" {
		in, err := a.st.callupPlayers(r.Context(), id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		for _, p := range in {
			pid := strconv.FormatInt(p.ID, 10)
			y, err := strconv.Atoi(r.PostFormValue("y_" + pid))
			if err != nil || y < 0 || y > 2 {
				y = 0
			}
			games, err := strconv.Atoi(r.PostFormValue("rg_" + pid))
			if err != nil || games < 1 || games > 20 {
				games = 1
			}
			goals, err := strconv.Atoi(r.PostFormValue("g_" + pid))
			if err != nil || goals < 0 || goals > 30 {
				goals = 0
			}
			if err := a.st.setMatchStats(r.Context(), t.ID, id, p.ID, goals, y, r.PostFormValue("r_"+pid) != "", games); err != nil {
				a.fail(w, r, err)
				return
			}
		}
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)+"#convocados"), http.StatusSeeOther)
}

// callupResult: resultado (los dos goles o ninguno) y observaciones.
func (a *app) callupResult(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	var gf, ga sql.NullInt64
	f, g := strings.TrimSpace(r.FormValue("goals_for")), strings.TrimSpace(r.FormValue("goals_against"))
	if (f == "") != (g == "") {
		httpError(w, r, "Resultado incompleto: faltan los goles de uno de los dos equipos.", http.StatusBadRequest)
		return
	}
	if f != "" {
		fv, err1 := strconv.ParseInt(f, 10, 32)
		gv, err2 := strconv.ParseInt(g, 10, 32)
		if err1 != nil || err2 != nil || fv < 0 || gv < 0 || fv > 99 || gv > 99 {
			httpError(w, r, "Resultado no válido.", http.StatusBadRequest)
			return
		}
		gf, ga = sql.NullInt64{Int64: fv, Valid: true}, sql.NullInt64{Int64: gv, Valid: true}
	}
	notes := strings.TrimSpace(r.FormValue("notes"))
	if len(notes) > 4000 {
		httpError(w, r, "Observaciones demasiado largas (máx. 4000 caracteres).", http.StatusBadRequest)
		return
	}
	if err := a.st.setResult(r.Context(), t.ID, id, gf, ga, notes); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)+"#resultado"), http.StatusSeeOther)
}

// callupLineup guarda formación y puestos (slot_0..slot_10 = id de jugador).
func (a *app) callupLineup(w http.ResponseWriter, r *http.Request, t Team) {
	id, ok := a.callupID(w, r)
	if !ok {
		return
	}
	f, ok := formationByName(r.FormValue("formation"))
	if !ok {
		httpError(w, r, "Formación no válida.", http.StatusBadRequest)
		return
	}
	slots := map[int]int64{}
	for i := range f.Slots {
		v := r.FormValue("slot_" + strconv.Itoa(i))
		if v == "" {
			continue
		}
		pid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpError(w, r, "Jugador no válido.", http.StatusBadRequest)
			return
		}
		slots[i] = pid
	}
	if err := a.st.saveLineup(r.Context(), t.ID, id, f.Name, slots); err != nil {
		if errors.Is(err, errDuplicatePlayer) {
			httpError(w, r, "Un jugador no puede ocupar dos puestos.", http.StatusConflict)
			return
		}
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(id, 10)+"#planteamiento"), http.StatusSeeOther)
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
		httpError(w, r, "Jugador no válido.", http.StatusBadRequest)
		return
	}
	if err := f(t.ID, cid, pid); err != nil {
		a.fail(w, r, err)
		return
	}
	// El ancla deja la página donde estaba tras añadir: se convoca a varios seguidos.
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(cid, 10)+"#lista"), http.StatusSeeOther)
}

// callupAdd añade de una vez los jugadores marcados en Disponibles y lleva a
// la lista de convocados, donde se marcan capitán y porteros.
func (a *app) callupAdd(w http.ResponseWriter, r *http.Request, t Team) {
	cid, ok := a.callupID(w, r)
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		httpError(w, r, "No se pudo leer el formulario. Vuelve a intentarlo.", http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, v := range r.PostForm["player"] {
		pid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpError(w, r, "Jugador no válido.", http.StatusBadRequest)
			return
		}
		ids = append(ids, pid)
	}
	if len(ids) == 0 {
		redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(cid, 10)+"#disponibles"), http.StatusSeeOther)
		return
	}
	// Un sancionado no puede jugar este partido (en amistosos no hay sanción:
	// Suspended no tiene entrada para ellos).
	disc, err := a.st.discipline(r.Context(), t.ID, today())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for _, pid := range ids {
		if reason := disc.Suspended[cid][pid]; reason != "" {
			p, err := a.st.player(r.Context(), t.ID, pid)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			httpError(w, r, fmt.Sprintf("%s está sancionado (%s) y no puede jugar este partido.", p.DisplayName(), reason),
				http.StatusConflict)
			return
		}
	}
	if err := a.st.addToCallup(r.Context(), t.ID, cid, ids); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/callups/"+strconv.FormatInt(cid, 10)+"#convocados"), http.StatusSeeOther)
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
	redirect(w, r, teamURL(t, "/callups"), http.StatusSeeOther)
}

func (a *app) boardsPage(w http.ResponseWriter, r *http.Request, t Team) {
	bs, err := a.st.boards(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cs, err := a.st.callups(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Solo convocatorias con planteamiento sirven de punto de partida.
	var withLineup []Callup
	for _, c := range cs {
		if c.Formation != "" {
			withLineup = append(withLineup, c)
		}
	}
	a.render(w, r, "boards", view{Team: &t, Tab: "boards", Title: "Pizarra", Data: map[string]any{
		"Boards": bs, "Formations": formations, "Callups": withLineup,
	}})
}

// createBoard parte de una formación ("f:4-4-2") o del planteamiento de una
// convocatoria ("c:<id>"), con los nombres de los jugadores colocados.
func (a *app) createBoard(w http.ResponseWriter, r *http.Request, t Team) {
	name := cut(strings.TrimSpace(r.FormValue("name")))
	if name == "" {
		httpError(w, r, "La pizarra necesita un nombre.", http.StatusBadRequest)
		return
	}
	var st BoardState
	origin := r.FormValue("origin")
	switch {
	case strings.HasPrefix(origin, "f:"):
		f, ok := formationByName(origin[2:])
		if !ok {
			httpError(w, r, "Formación no válida.", http.StatusBadRequest)
			return
		}
		st = stateFromFormation(f, nil)
	case strings.HasPrefix(origin, "c:"):
		cid, err := strconv.ParseInt(origin[2:], 10, 64)
		if err != nil {
			httpError(w, r, "Convocatoria no válida.", http.StatusBadRequest)
			return
		}
		c, err := a.st.callup(r.Context(), t.ID, cid)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		f, ok := formationByName(c.Formation)
		if !ok {
			httpError(w, r, "Esa convocatoria no tiene planteamiento.", http.StatusBadRequest)
			return
		}
		assigned, err := a.st.lineup(r.Context(), cid)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		in, err := a.st.callupPlayers(r.Context(), cid)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		byID := map[int64]Player{}
		for _, p := range in {
			byID[p.ID] = p
		}
		names := map[int]string{}
		for slot, pid := range assigned {
			names[slot] = byID[pid].DisplayName()
		}
		st = stateFromFormation(f, names)
	default:
		st = BoardState{Ball: BoardPoint{pitchW / 2, pitchL / 2}}
		_ = st.sanitize()
	}
	id, err := a.st.createBoard(r.Context(), t.ID, name, st)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/boards/"+strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

func (a *app) boardPage(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "board")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := a.st.board(r.Context(), t.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, "board", view{Team: &t, Tab: "boards", Title: b.Name, Data: b})
}

// saveBoard recibe JSON desde board.js ({name, state}); responde 204.
func (a *app) saveBoard(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "board")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var in struct {
		Name  string     `json:"name"`
		State BoardState `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		httpError(w, r, "Pizarra no válida.", http.StatusBadRequest)
		return
	}
	in.Name = cut(strings.TrimSpace(in.Name))
	if in.Name == "" {
		httpError(w, r, "La pizarra necesita un nombre.", http.StatusBadRequest)
		return
	}
	if err := in.State.sanitize(); err != nil {
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.st.saveBoard(r.Context(), t.ID, id, in.Name, in.State); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) deleteBoard(w http.ResponseWriter, r *http.Request, t Team) {
	id, err := pathID(r, "board")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.st.deleteBoard(r.Context(), t.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/boards"), http.StatusSeeOther)
}

func (a *app) settingsPage(w http.ResponseWriter, r *http.Request, t Team) {
	cs, err := a.st.coaches(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Tras subir la plantilla o guardar su posición se vuelve con la sección abierta.
	data := map[string]any{"Coaches": cs, "OpenImage": r.URL.Query().Has("imagen")}
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
		httpError(w, r, "Email no válido.", http.StatusBadRequest)
		return
	}
	if err := a.st.addCoach(r.Context(), t.ID, email); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
}

func (a *app) removeCoach(w http.ResponseWriter, r *http.Request, t Team) {
	email := normEmail(r.FormValue("email"))
	// Quitarse a uno mismo dejaría el equipo huérfano o al usuario fuera sin querer.
	if email == currentUser(r.Context()).Email {
		httpError(w, r, "No puedes quitarte a ti mismo.", http.StatusBadRequest)
		return
	}
	if err := a.st.removeCoach(r.Context(), t.ID, email); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/settings"), http.StatusSeeOther)
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
		httpError(w, r, "Imagen no válida o mayor de 5 MB.", http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()
	img, err := io.ReadAll(f)
	if err != nil {
		httpError(w, r, "Imagen no válida o mayor de 5 MB.", http.StatusBadRequest)
		return
	}
	// El tipo se deduce del contenido, no de lo que diga el cliente: luego se
	// sirve con ese Content-Type.
	ct := http.DetectContentType(img)
	if ct != "image/png" && ct != "image/jpeg" && ct != "image/webp" {
		httpError(w, r, "La imagen tiene que ser PNG, JPEG o WebP.", http.StatusBadRequest)
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
	redirect(w, r, teamURL(t, "/settings?imagen#imagen"), http.StatusSeeOther)
}

func (a *app) saveLayout(w http.ResponseWriter, r *http.Request, t Team) {
	var l Layout
	if err := json.Unmarshal([]byte(r.FormValue("layout")), &l); err != nil || !l.valid() {
		httpError(w, r, "Posición de la imagen no válida.", http.StatusBadRequest)
		return
	}
	b, _ := json.Marshal(l)
	if err := a.st.saveLayout(r.Context(), t.ID, b); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, teamURL(t, "/settings?imagen#imagen"), http.StatusSeeOther)
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
