package main

import (
	"context"
	"database/sql"
	"time"
)

// Profile reúne los datos de un jugador en la temporada para hablar con él:
// solo lo suyo, nada del resto del equipo.
type Profile struct {
	Player      Player
	Since       time.Time
	ByStatus    map[Status]int
	Sessions    int // entrenos con lista en los que estaba
	Recent      []Mark
	Matches     []ProfileMatch
	Official    int // partidos oficiales del equipo en la temporada
	CalledUp    int // de esos, en cuántos estuvo convocado
	Goals       int
	YellowTotal int // todas las amarillas en oficiales (incluida la doble)
	Reds        int
}

type ProfileMatch struct {
	Callup
	Goals      int
	Yellow     int
	Red        bool
	IsCaptain  bool
	Goalkeeper bool
}

// Came: entrenos a los que vino (a su hora o tarde).
func (p Profile) Came() int { return p.ByStatus[Presente] + p.ByStatus[Tarde] }

// AttendancePct: vino (a su hora o tarde) sobre entrenos con lista.
func (p Profile) AttendancePct() int {
	if p.Sessions == 0 {
		return 0
	}
	return (p.ByStatus[Presente] + p.ByStatus[Tarde]) * 100 / p.Sessions
}

func (s store) profile(ctx context.Context, teamID, playerID int64, today time.Time) (Profile, error) {
	p, err := s.player(ctx, teamID, playerID)
	if err != nil {
		return Profile{}, err
	}
	since := time.Date(seasonOf(today), time.August, 1, 0, 0, 0, 0, time.UTC)
	pr := Profile{Player: p, Since: since, ByStatus: map[Status]int{}}

	rows, err := s.db.QueryContext(ctx, `SELECT t.day, a.status
		FROM attendance a JOIN trainings t ON t.id = a.training_id
		WHERE t.team_id = $1 AND a.player_id = $2 AND t.day >= $3
		ORDER BY t.day DESC, t.id DESC`, teamID, playerID, since)
	if err != nil {
		return Profile{}, err
	}
	for rows.Next() {
		var m Mark
		if err := rows.Scan(&m.Day, &m.Status); err != nil {
			_ = rows.Close()
			return Profile{}, err
		}
		pr.ByStatus[m.Status]++
		pr.Sessions++
		if len(pr.Recent) < 12 {
			pr.Recent = append(pr.Recent, m)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return Profile{}, err
	}
	// Los últimos entrenos, del más antiguo al más reciente, como el ciclo.
	for i, j := 0, len(pr.Recent)-1; i < j; i, j = i+1, j-1 {
		pr.Recent[i], pr.Recent[j] = pr.Recent[j], pr.Recent[i]
	}

	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM callups
		WHERE team_id = $1 AND NOT friendly AND day >= $2`, teamID, since).Scan(&pr.Official); err != nil {
		return Profile{}, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT `+callupCols+`, cp.goals, cp.yellow, cp.red, cp.goalkeeper
		FROM callups c JOIN callup_players cp ON cp.callup_id = c.id
		WHERE c.team_id = $1 AND cp.player_id = $2 AND c.day >= $3
		ORDER BY c.day DESC, c.id DESC`, teamID, playerID, since)
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m ProfileMatch
		if err := scanCallup(rows, &m.Callup, &m.Goals, &m.Yellow, &m.Red, &m.Goalkeeper); err != nil {
			return Profile{}, err
		}
		m.IsCaptain = m.Captain == (sql.NullInt64{Int64: playerID, Valid: true})
		pr.Matches = append(pr.Matches, m)
		if m.Friendly {
			continue
		}
		pr.CalledUp++
		pr.Goals += m.Goals
		pr.YellowTotal += m.Yellow
		if m.Red {
			pr.Reds++
		}
	}
	return pr, rows.Err()
}
