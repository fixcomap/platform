package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// openDB abre un pool compartido. Neon cierra conexiones inactivas y Cloud Run
// congela la CPU entre peticiones: conexiones de vida corta evitan reutilizar
// sockets muertos tras un escalado desde cero.
func openDB(ctx context.Context) (*sql.DB, error) {
	dsn, err := databaseURL()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrate aplica en orden los .sql embebidos que falten. El advisory lock
// serializa el arranque de varias instancias a la vez (max 2 en Cloud Run).
func migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	const lockID = 7349001
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockID) }()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := conn.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)", name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

type Team struct {
	ID   int64
	Name string
}

type Player struct {
	ID        int64
	FirstName string
	LastName  string
	Nickname  string
	Number    sql.NullInt64
	Active    bool
	// Solo en listados con estadística.
	Attended, Trainings, Injured, Justified int
	// Entrenos del ciclo (desde el último partido); solo donde se muestra.
	Cycle []Mark
	// Tarjetas y portero en un partido; solo en la lista de convocados.
	Yellow     int
	Red        bool
	RedGames   int
	Goalkeeper bool
	// Goles: en un partido (convocados) o en la temporada (Jugadores).
	Goals int
	// Disciplina: motivo si está sancionado para el partido que se mira y
	// amarillas del ciclo acumuladas antes de él (4 = apercibido).
	Suspension   string
	CycleYellows int
	// Faltas seguidas más recientes sin contar lesiones (2+ = aviso).
	Streak int
	// Llegadas tarde en sus últimos lateWindow entrenos (lateAlert+ = aviso).
	Late int
}

// Status de asistencia. Para el porcentaje, cualquier ausencia es falta:
// lesionado y justificado solo explican el motivo.
type Status string

const (
	Presente      Status = "presente"
	Tarde         Status = "tarde" // vino, pero tarde: cuenta como asistencia
	Lesionado     Status = "lesionado"
	Justificado   Status = "justificado"
	Injustificado Status = "injustificado"
)

var statuses = []Status{Presente, Tarde, Lesionado, Justificado, Injustificado}

// Attended: vino (a su hora o tarde).
func (s Status) Attended() bool { return s == Presente || s == Tarde }

func validStatus(s Status) bool {
	for _, v := range statuses {
		if v == s {
			return true
		}
	}
	return false
}

// Mark es la asistencia de un jugador a un entreno del ciclo. Sin marca
// (Status vacío) = no se pasó lista con él en el equipo.
type Mark struct {
	Day    time.Time
	Status Status
}

// DisplayName es el nombre que va en la lista y en la imagen: el apodo si lo
// hay, como en las convocatorias que ya se hacen a mano.
func (p Player) DisplayName() string {
	if p.Nickname != "" {
		return p.Nickname
	}
	return p.FirstName
}

func (p Player) Percent() int {
	if p.Trainings == 0 {
		return 0
	}
	return p.Attended * 100 / p.Trainings
}

type Training struct {
	ID      int64
	Day     time.Time
	Notes   string
	Present int
	Total   int
}

// Callup es una convocatoria y, a la vez, el partido.
type Callup struct {
	ID       int64
	Day      time.Time
	Rival    string
	Place    string
	Kickoff  string // "HH:MM" o vacío
	Meet     string // citación, "HH:MM" o vacío
	Home     sql.NullBool
	GoalsFor sql.NullInt64
	GoalsAg  sql.NullInt64
	Captain  sql.NullInt64
	Friendly bool
	Notes    string
	Count    int
}

// Played: hay resultado cargado.
func (c Callup) Played() bool { return c.GoalsFor.Valid && c.GoalsAg.Valid }

// Outcome: G, E o P desde el punto de vista del equipo.
func (c Callup) Outcome() string {
	switch {
	case !c.Played():
		return ""
	case c.GoalsFor.Int64 > c.GoalsAg.Int64:
		return "G"
	case c.GoalsFor.Int64 < c.GoalsAg.Int64:
		return "P"
	}
	return "E"
}

// Score en el orden habitual: el local primero.
func (c Callup) Score() string {
	if !c.Played() {
		return ""
	}
	a, b := c.GoalsFor.Int64, c.GoalsAg.Int64
	if c.Home.Valid && !c.Home.Bool {
		a, b = b, a
	}
	return fmt.Sprintf("%d-%d", a, b)
}

const callupCols = `c.id, c.day, c.rival, c.place, COALESCE(c.kickoff, ''), COALESCE(c.meet_time, ''),
	c.home, c.goals_for, c.goals_against, c.captain_id, c.friendly, c.notes`

func scanCallup(sc interface{ Scan(...any) error }, c *Callup, extra ...any) error {
	return sc.Scan(append([]any{&c.ID, &c.Day, &c.Rival, &c.Place, &c.Kickoff, &c.Meet,
		&c.Home, &c.GoalsFor, &c.GoalsAg, &c.Captain, &c.Friendly, &c.Notes}, extra...)...)
}

type store struct{ db *sql.DB }

// canAccess: admin global o entrenador del equipo.
func (s store) canAccess(ctx context.Context, email string, admin bool, teamID int64) (bool, error) {
	var ok bool
	q := "SELECT EXISTS (SELECT 1 FROM team_coaches WHERE team_id = $1 AND email = $2)"
	if admin {
		q = "SELECT EXISTS (SELECT 1 FROM teams WHERE id = $1 AND $2 <> '')"
	}
	err := s.db.QueryRowContext(ctx, q, teamID, email).Scan(&ok)
	return ok, err
}

func (s store) teams(ctx context.Context, email string, admin bool) ([]Team, error) {
	q := `SELECT t.id, t.name FROM teams t JOIN team_coaches c ON c.team_id = t.id
	      WHERE c.email = $1 ORDER BY t.name`
	if admin {
		q = "SELECT id, name FROM teams WHERE $1 <> '' ORDER BY name"
	}
	rows, err := s.db.QueryContext(ctx, q, email)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s store) team(ctx context.Context, id int64) (Team, error) {
	t := Team{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT name FROM teams WHERE id = $1", id).Scan(&t.Name)
	return t, err
}

// createTeam deja a quien lo crea como entrenador, para que lo siga viendo si
// deja de ser admin.
func (s store) createTeam(ctx context.Context, name, email string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if err := tx.QueryRowContext(ctx, "INSERT INTO teams (name) VALUES ($1) RETURNING id", name).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO team_coaches (team_id, email) VALUES ($1, $2)", id, email); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s store) coaches(ctx context.Context, teamID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT email FROM team_coaches WHERE team_id = $1 ORDER BY email", teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s store) addCoach(ctx context.Context, teamID int64, email string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO team_coaches (team_id, email) VALUES ($1, $2) ON CONFLICT DO NOTHING", teamID, email)
	return err
}

func (s store) removeCoach(ctx context.Context, teamID int64, email string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM team_coaches WHERE team_id = $1 AND email = $2", teamID, email)
	return err
}

// players devuelve la plantilla con su asistencia: entrenos a los que vino
// sobre entrenos en los que se pasó lista estando él en el equipo.
func (s store) players(ctx context.Context, teamID int64, onlyActive bool) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.first_name, p.last_name, p.nickname, p.number, p.active,
		       count(a.*) FILTER (WHERE a.status IN ('presente', 'tarde')), count(a.*),
		       count(a.*) FILTER (WHERE a.status = 'lesionado'),
		       count(a.*) FILTER (WHERE a.status = 'justificado')
		FROM players p LEFT JOIN attendance a ON a.player_id = p.id
		WHERE p.team_id = $1 AND (p.active OR NOT $2)
		GROUP BY p.id
		ORDER BY NOT p.active, p.number NULLS LAST, p.first_name`, teamID, onlyActive)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.FirstName, &p.LastName, &p.Nickname, &p.Number, &p.Active,
			&p.Attended, &p.Trainings, &p.Injured, &p.Justified); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	streaks, late, err := s.attendanceAlerts(ctx, teamID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Streak = streaks[out[i].ID]
		out[i].Late = late[out[i].ID]
	}
	return out, nil
}

const (
	lateWindow = 8 // entrenos recientes que se miran
	lateAlert  = 5 // tardes en esa ventana para avisar
)

// attendanceAlerts calcula, por jugador:
//   - streak: ausencias seguidas desde la última vez que vino. Una lesión no
//     corta la racha ni suma: es ajena a su compromiso.
//   - late: llegadas tarde en sus últimos lateWindow entrenos con lista.
func (s store) attendanceAlerts(ctx context.Context, teamID int64) (map[int64]int, map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.player_id, a.status
		FROM attendance a JOIN trainings t ON t.id = a.training_id
		WHERE t.team_id = $1
		ORDER BY a.player_id, t.day DESC, t.id DESC`, teamID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	streak, late := map[int64]int{}, map[int64]int{}
	seen := map[int64]int{}
	streakDone := map[int64]bool{}
	for rows.Next() {
		var pid int64
		var st Status
		if err := rows.Scan(&pid, &st); err != nil {
			return nil, nil, err
		}
		if seen[pid] < lateWindow {
			seen[pid]++
			if st == Tarde {
				late[pid]++
			}
		}
		switch {
		case streakDone[pid] || st == Lesionado:
		case st.Attended():
			streakDone[pid] = true
		default:
			streak[pid]++
		}
	}
	return streak, late, rows.Err()
}

func (s store) player(ctx context.Context, teamID, id int64) (Player, error) {
	p := Player{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT first_name, last_name, nickname, number, active
		FROM players WHERE team_id = $1 AND id = $2`, teamID, id).
		Scan(&p.FirstName, &p.LastName, &p.Nickname, &p.Number, &p.Active)
	return p, err
}

func (s store) savePlayer(ctx context.Context, teamID int64, p Player) error {
	if p.ID == 0 {
		_, err := s.db.ExecContext(ctx, `INSERT INTO players (team_id, first_name, last_name, nickname, number)
			VALUES ($1, $2, $3, $4, $5)`, teamID, p.FirstName, p.LastName, p.Nickname, p.Number)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE players SET first_name = $3, last_name = $4, nickname = $5,
		number = $6, active = $7 WHERE team_id = $1 AND id = $2`,
		teamID, p.ID, p.FirstName, p.LastName, p.Nickname, p.Number, p.Active)
	return err
}

func (s store) trainings(ctx context.Context, teamID int64) ([]Training, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.day, t.notes, count(a.*) FILTER (WHERE a.status IN ('presente', 'tarde')), count(a.*)
		FROM trainings t LEFT JOIN attendance a ON a.training_id = t.id
		WHERE t.team_id = $1 GROUP BY t.id ORDER BY t.day DESC, t.id DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Training
	for rows.Next() {
		var t Training
		if err := rows.Scan(&t.ID, &t.Day, &t.Notes, &t.Present, &t.Total); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s store) training(ctx context.Context, teamID, id int64) (Training, error) {
	t := Training{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT day, notes FROM trainings WHERE team_id = $1 AND id = $2",
		teamID, id).Scan(&t.Day, &t.Notes)
	return t, err
}

func (s store) createTraining(ctx context.Context, teamID int64, day time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "INSERT INTO trainings (team_id, day) VALUES ($1, $2) RETURNING id",
		teamID, day).Scan(&id)
	return id, err
}

func (s store) deleteTraining(ctx context.Context, teamID, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM trainings WHERE team_id = $1 AND id = $2", teamID, id)
	return err
}

// attendance: player_id -> estado, solo para los que ya tienen marca.
func (s store) attendance(ctx context.Context, trainingID int64) (map[int64]Status, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT player_id, status FROM attendance WHERE training_id = $1", trainingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]Status{}
	for rows.Next() {
		var id int64
		var st Status
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// saveAttendance guarda la lista completa: cada activo con su estado y, si no
// llega ninguno, ausencia no justificada. Así toda falta queda registrada.
func (s store) saveAttendance(ctx context.Context, teamID, trainingID int64, notes string, status map[int64]Status) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, "UPDATE trainings SET notes = $3 WHERE team_id = $1 AND id = $2",
		teamID, trainingID, notes)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM players WHERE team_id = $1 AND active", teamID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		st := status[id]
		if !validStatus(st) {
			st = Injustificado
		}
		// present se sigue escribiendo mientras exista (ver migración 002).
		if _, err := tx.ExecContext(ctx, `INSERT INTO attendance (training_id, player_id, status, present)
			VALUES ($1, $2, $3, $4) ON CONFLICT (training_id, player_id)
			DO UPDATE SET status = EXCLUDED.status, present = EXCLUDED.present`,
			trainingID, id, st, st.Attended()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// cycle devuelve los entrenos del ciclo que acaba en end y las marcas de cada
// jugador. El ciclo empieza tras el último partido (convocatoria) anterior a
// end: con una fecha libre no hay convocatoria y el ciclo abarca las dos
// semanas. Sin partidos previos, los últimos 14 días. includeEnd: el día end
// cuenta (hoy en Jugadores; el día del partido no).
func (s store) cycle(ctx context.Context, teamID int64, end time.Time, includeEnd bool) (time.Time, bool, []Training, map[int64]map[int64]Status, error) {
	var last sql.NullTime
	if err := s.db.QueryRowContext(ctx, "SELECT max(day) FROM callups WHERE team_id = $1 AND day < $2",
		teamID, end).Scan(&last); err != nil {
		return time.Time{}, false, nil, nil, err
	}
	start := end.AddDate(0, 0, -14)
	if last.Valid {
		start = last.Time
	}
	cmp := "<"
	if includeEnd {
		cmp = "<="
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.day, a.player_id, a.status
		FROM trainings t LEFT JOIN attendance a ON a.training_id = t.id
		WHERE t.team_id = $1 AND t.day > $2 AND t.day `+cmp+` $3
		ORDER BY t.day, t.id`, teamID, start, end)
	if err != nil {
		return time.Time{}, false, nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var ts []Training
	marks := map[int64]map[int64]Status{} // player -> training -> estado
	for rows.Next() {
		var tid int64
		var day time.Time
		var pid sql.NullInt64
		var st sql.NullString
		if err := rows.Scan(&tid, &day, &pid, &st); err != nil {
			return time.Time{}, false, nil, nil, err
		}
		if len(ts) == 0 || ts[len(ts)-1].ID != tid {
			ts = append(ts, Training{ID: tid, Day: day})
		}
		if pid.Valid {
			if marks[pid.Int64] == nil {
				marks[pid.Int64] = map[int64]Status{}
			}
			marks[pid.Int64][tid] = Status(st.String)
		}
	}
	return start, last.Valid, ts, marks, rows.Err()
}

func (s store) callups(ctx context.Context, teamID int64) ([]Callup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+callupCols+`, count(cp.*)
		FROM callups c LEFT JOIN callup_players cp ON cp.callup_id = c.id
		WHERE c.team_id = $1 GROUP BY c.id ORDER BY c.day DESC, c.id DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Callup
	for rows.Next() {
		var c Callup
		if err := scanCallup(rows, &c, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s store) callup(ctx context.Context, teamID, id int64) (Callup, error) {
	var c Callup
	err := scanCallup(s.db.QueryRowContext(ctx, "SELECT "+callupCols+" FROM callups c WHERE c.team_id = $1 AND c.id = $2",
		teamID, id), &c)
	return c, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s store) createCallup(ctx context.Context, teamID int64, c Callup) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO callups (team_id, day, rival, place, kickoff, meet_time, home, friendly)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, teamID, c.Day, c.Rival, c.Place,
		nullIfEmpty(c.Kickoff), nullIfEmpty(c.Meet), c.Home, c.Friendly).Scan(&id)
	return id, err
}

// updateCallup guarda datos del partido y resultado (los goles, ambos o ninguno).
func (s store) updateCallup(ctx context.Context, teamID int64, c Callup) error {
	res, err := s.db.ExecContext(ctx, `UPDATE callups SET day = $3, rival = $4, place = $5, kickoff = $6,
		meet_time = $7, home = $8, goals_for = $9, goals_against = $10, friendly = $11
		WHERE team_id = $1 AND id = $2`,
		teamID, c.ID, c.Day, c.Rival, c.Place, nullIfEmpty(c.Kickoff), nullIfEmpty(c.Meet), c.Home,
		c.GoalsFor, c.GoalsAg, c.Friendly)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s store) setCallupNotes(ctx context.Context, teamID, callupID int64, notes string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE callups SET notes = $3 WHERE team_id = $1 AND id = $2", teamID, callupID, notes)
	return err
}

// setMatchStats: goles y tarjetas de un convocado; el UPDATE solo encuentra
// la fila si convocatoria y jugador son de este equipo.
func (s store) setMatchStats(ctx context.Context, teamID, callupID, playerID int64, goals, yellow int, red bool, redGames int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE callup_players cp SET goals = $4, yellow = $5, red = $6, red_games = $7
		FROM callups c WHERE cp.callup_id = c.id AND c.team_id = $1 AND c.id = $2 AND cp.player_id = $3`,
		teamID, callupID, playerID, goals, yellow, red, redGames)
	return err
}

func (s store) deleteCallup(ctx context.Context, teamID, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM callups WHERE team_id = $1 AND id = $2", teamID, id)
	return err
}

// callupPlayers: convocados en el orden en que se añadieron.
type Scorer struct {
	Player Player
	Goals  int
}

// scorers: goleadores de la temporada (desde el 1 de agosto) en partidos
// oficiales, de más a menos goles.
func (s store) scorers(ctx context.Context, teamID int64, today time.Time) ([]Scorer, error) {
	start := time.Date(seasonOf(today), time.August, 1, 0, 0, 0, 0, time.UTC)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.first_name, p.nickname, sum(cp.goals)::int
		FROM callup_players cp JOIN callups c ON c.id = cp.callup_id JOIN players p ON p.id = cp.player_id
		WHERE c.team_id = $1 AND NOT c.friendly AND c.day >= $2 AND cp.goals > 0
		GROUP BY p.id ORDER BY 4 DESC, p.first_name`, teamID, start)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Scorer
	for rows.Next() {
		var sc Scorer
		if err := rows.Scan(&sc.Player.ID, &sc.Player.FirstName, &sc.Player.Nickname, &sc.Goals); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s store) callupPlayers(ctx context.Context, callupID int64) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.first_name, p.last_name, p.nickname, p.number, p.active, cp.yellow, cp.red, cp.goalkeeper, cp.red_games, cp.goals
		FROM callup_players cp JOIN players p ON p.id = cp.player_id
		WHERE cp.callup_id = $1 ORDER BY cp.position`, callupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.FirstName, &p.LastName, &p.Nickname, &p.Number, &p.Active,
			&p.Yellow, &p.Red, &p.Goalkeeper, &p.RedGames, &p.Goals); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// addToCallup comprueba en la misma sentencia que convocatoria y jugador son
// del equipo: los ids llegan del formulario y no son de fiar.
func (s store) addToCallup(ctx context.Context, teamID, callupID, playerID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO callup_players (callup_id, player_id, position)
		SELECT c.id, p.id, COALESCE((SELECT max(position) FROM callup_players WHERE callup_id = c.id), 0) + 1
		FROM callups c JOIN players p ON p.team_id = c.team_id
		WHERE c.team_id = $1 AND c.id = $2 AND p.id = $3
		ON CONFLICT DO NOTHING`, teamID, callupID, playerID)
	return err
}

// removeFromCallup quita al jugador y, si era el capitán, deja la
// convocatoria sin capitán: hay que volver a elegirlo.
func (s store) removeFromCallup(ctx context.Context, teamID, callupID, playerID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM callup_players cp USING callups c
		WHERE cp.callup_id = c.id AND c.team_id = $1 AND c.id = $2 AND cp.player_id = $3`,
		teamID, callupID, playerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE callups SET captain_id = NULL
		WHERE team_id = $1 AND id = $2 AND captain_id = $3`, teamID, callupID, playerID); err != nil {
		return err
	}
	return tx.Commit()
}

// setRoles fija capitán (0 = ninguno) y porteros. Ambos deben ser convocados
// de esta convocatoria; lo garantizan los JOIN/EXISTS contra callup_players.
func (s store) setRoles(ctx context.Context, teamID, callupID, captain int64, goalkeepers []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE callups c SET captain_id = CASE WHEN EXISTS (
			SELECT 1 FROM callup_players WHERE callup_id = c.id AND player_id = $3) THEN $3::bigint END
		WHERE c.team_id = $1 AND c.id = $2`, teamID, callupID, captain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE callup_players SET goalkeeper = (player_id = ANY($2))
		WHERE callup_id = $1`, callupID, goalkeepers); err != nil {
		return err
	}
	return tx.Commit()
}

type Template struct {
	Image       []byte
	ContentType string
	Layout      []byte
	UpdatedAt   time.Time
}

func (s store) template(ctx context.Context, teamID int64) (Template, error) {
	var t Template
	err := s.db.QueryRowContext(ctx, `SELECT image, content_type, layout, updated_at
		FROM team_templates WHERE team_id = $1`, teamID).Scan(&t.Image, &t.ContentType, &t.Layout, &t.UpdatedAt)
	return t, err
}

func (s store) templateLayout(ctx context.Context, teamID int64) ([]byte, time.Time, error) {
	var layout []byte
	var at time.Time
	err := s.db.QueryRowContext(ctx, "SELECT layout, updated_at FROM team_templates WHERE team_id = $1",
		teamID).Scan(&layout, &at)
	return layout, at, err
}

func (s store) saveTemplate(ctx context.Context, teamID int64, image []byte, contentType string, layout []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO team_templates (team_id, image, content_type, layout)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (team_id) DO UPDATE SET image = EXCLUDED.image, content_type = EXCLUDED.content_type,
		layout = EXCLUDED.layout, updated_at = now()`, teamID, image, contentType, layout)
	return err
}

func (s store) saveLayout(ctx context.Context, teamID int64, layout []byte) error {
	res, err := s.db.ExecContext(ctx, "UPDATE team_templates SET layout = $2, updated_at = now() WHERE team_id = $1",
		teamID, layout)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
