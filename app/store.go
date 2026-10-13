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
	Attended, Trainings int
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

type Callup struct {
	ID    int64
	Day   time.Time
	Rival string
	Place string
	Count int
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

func (s store) hasAnyTeam(ctx context.Context, email string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM team_coaches WHERE email = $1)", email).Scan(&ok)
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
		       count(a.*) FILTER (WHERE a.present), count(a.*)
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
			&p.Attended, &p.Trainings); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
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
		SELECT t.id, t.day, t.notes, count(a.*) FILTER (WHERE a.present), count(a.*)
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

// attendance: player_id -> present, solo para los que ya tienen marca.
func (s store) attendance(ctx context.Context, trainingID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT player_id, present FROM attendance WHERE training_id = $1", trainingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var present bool
		if err := rows.Scan(&id, &present); err != nil {
			return nil, err
		}
		out[id] = present
	}
	return out, rows.Err()
}

// saveAttendance guarda la lista completa: los activos marcados vinieron, el
// resto no. Así "no vino" queda registrado y cuenta en el porcentaje.
func (s store) saveAttendance(ctx context.Context, teamID, trainingID int64, notes string, present map[int64]bool) error {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO attendance (training_id, player_id, present)
			VALUES ($1, $2, $3) ON CONFLICT (training_id, player_id) DO UPDATE SET present = EXCLUDED.present`,
			trainingID, id, present[id]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s store) callups(ctx context.Context, teamID int64) ([]Callup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.day, c.rival, c.place, count(cp.*)
		FROM callups c LEFT JOIN callup_players cp ON cp.callup_id = c.id
		WHERE c.team_id = $1 GROUP BY c.id ORDER BY c.day DESC, c.id DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Callup
	for rows.Next() {
		var c Callup
		if err := rows.Scan(&c.ID, &c.Day, &c.Rival, &c.Place, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s store) callup(ctx context.Context, teamID, id int64) (Callup, error) {
	c := Callup{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT day, rival, place FROM callups WHERE team_id = $1 AND id = $2",
		teamID, id).Scan(&c.Day, &c.Rival, &c.Place)
	return c, err
}

func (s store) createCallup(ctx context.Context, teamID int64, c Callup) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO callups (team_id, day, rival, place)
		VALUES ($1, $2, $3, $4) RETURNING id`, teamID, c.Day, c.Rival, c.Place).Scan(&id)
	return id, err
}

func (s store) deleteCallup(ctx context.Context, teamID, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM callups WHERE team_id = $1 AND id = $2", teamID, id)
	return err
}

// callupPlayers: convocados en el orden en que se añadieron.
func (s store) callupPlayers(ctx context.Context, callupID int64) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.first_name, p.last_name, p.nickname, p.number, p.active
		FROM callup_players cp JOIN players p ON p.id = cp.player_id
		WHERE cp.callup_id = $1 ORDER BY cp.position`, callupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.FirstName, &p.LastName, &p.Nickname, &p.Number, &p.Active); err != nil {
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

func (s store) removeFromCallup(ctx context.Context, teamID, callupID, playerID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM callup_players cp USING callups c
		WHERE cp.callup_id = c.id AND c.team_id = $1 AND c.id = $2 AND cp.player_id = $3`,
		teamID, callupID, playerID)
	return err
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
