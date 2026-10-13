package main

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// Reglas de sanción (acordadas con el cuerpo técnico, criterio RFEF habitual):
//   - Ciclo de amarillas: cada 5 amarillas en la temporada, 1 partido.
//   - Doble amarilla en un partido (expulsión): 1 partido; esas 2 no suman al ciclo.
//   - Roja directa: red_games partidos (1 por defecto); una amarilla previa sí suma.
//   - La temporada empieza el 1 de agosto: el ciclo de amarillas se reinicia.
//   - Los amistosos no cuentan tarjetas ni sirven para cumplir sanción.
// Una sanción se cumple en los siguientes partidos oficiales del equipo,
// juegue o no el sancionado.

const yellowCycle = 5

type cardRow struct {
	Yellow   int
	Red      bool
	RedGames int
}

type matchCards struct {
	ID       int64
	Day      time.Time
	Friendly bool
	Cards    map[int64]cardRow // player -> tarjetas
}

// Discipline es el estado disciplinario calculado para todo el equipo.
type Discipline struct {
	// Suspended[callup][player] = motivo: no puede jugar ese partido.
	Suspended map[int64]map[int64]string
	// YellowsBefore[callup][player]: amarillas del ciclo antes de ese partido.
	YellowsBefore map[int64]map[int64]int
	// Next: sancionados para el próximo partido oficial aún no creado.
	Next map[int64]string
	// Yellows: amarillas acumuladas en la temporada en curso (para la ficha).
	Yellows map[int64]int
}

func seasonOf(d time.Time) int {
	if d.Month() >= time.August {
		return d.Year()
	}
	return d.Year() - 1
}

type pending struct {
	games  int
	reason string
}

// computeDiscipline recorre los partidos en orden cronológico.
func computeDiscipline(matches []matchCards, today time.Time) Discipline {
	d := Discipline{Suspended: map[int64]map[int64]string{}, YellowsBefore: map[int64]map[int64]int{},
		Next: map[int64]string{}, Yellows: map[int64]int{}}
	acc := map[int64]int{}
	pend := map[int64]*pending{}
	season := -1
	for _, m := range matches {
		if m.Friendly {
			continue
		}
		if s := seasonOf(m.Day); s != season {
			season, acc = s, map[int64]int{}
		}
		// Primero se cumplen las sanciones pendientes con este partido.
		d.Suspended[m.ID] = map[int64]string{}
		for pid, p := range pend {
			if p.games > 0 {
				d.Suspended[m.ID][pid] = p.reason
				p.games--
			}
		}
		d.YellowsBefore[m.ID] = map[int64]int{}
		for pid, n := range acc {
			d.YellowsBefore[m.ID][pid] = n
		}
		// Después, las tarjetas de este partido generan nuevas sanciones.
		for pid, c := range m.Cards {
			add := func(games int, reason string) {
				if pend[pid] == nil {
					pend[pid] = &pending{}
				}
				pend[pid].games += games
				pend[pid].reason = reason
			}
			switch {
			case c.Yellow == 2:
				add(1, "doble amarilla")
			case c.Yellow == 1:
				acc[pid]++
				if acc[pid]%yellowCycle == 0 {
					add(1, strconv.Itoa(acc[pid])+"ª amarilla")
				}
			}
			if c.Red {
				add(max(c.RedGames, 1), "roja directa")
			}
		}
	}
	for pid, p := range pend {
		if p.games > 0 {
			d.Next[pid] = p.reason
		}
	}
	if season == seasonOf(today) {
		d.Yellows = acc
	}
	return d
}

func (s store) discipline(ctx context.Context, teamID int64, today time.Time) (Discipline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.day, c.friendly, cp.player_id, cp.yellow, cp.red, cp.red_games
		FROM callups c LEFT JOIN callup_players cp ON cp.callup_id = c.id
		WHERE c.team_id = $1 ORDER BY c.day, c.id`, teamID)
	if err != nil {
		return Discipline{}, err
	}
	defer func() { _ = rows.Close() }()
	var ms []matchCards
	for rows.Next() {
		var m matchCards
		var pid, yellow, redGames sql.NullInt64
		var red sql.NullBool
		if err := rows.Scan(&m.ID, &m.Day, &m.Friendly, &pid, &yellow, &red, &redGames); err != nil {
			return Discipline{}, err
		}
		if len(ms) == 0 || ms[len(ms)-1].ID != m.ID {
			m.Cards = map[int64]cardRow{}
			ms = append(ms, m)
		}
		if pid.Valid && (yellow.Int64 > 0 || red.Bool) {
			ms[len(ms)-1].Cards[pid.Int64] = cardRow{Yellow: int(yellow.Int64), Red: red.Bool, RedGames: int(redGames.Int64)}
		}
	}
	if err := rows.Err(); err != nil {
		return Discipline{}, err
	}
	return computeDiscipline(ms, today), nil
}
