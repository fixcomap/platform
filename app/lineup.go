package main

import (
	"context"
	"database/sql"
	"errors"
)

// Slot: un puesto de la formación. X/Y en % del campo, atacando hacia arriba
// (portero abajo). Fits: posiciones del jugador que encajan en el puesto, por
// orden de preferencia, para sugerir a quién poner.
type Slot struct {
	Code string
	X, Y float64
	Fits []string
}

type Formation struct {
	Name  string
	Slots []Slot
}

func back4() []Slot {
	return []Slot{
		{"POR", 50, 90, []string{"POR"}},
		{"LD", 85, 70, []string{"LD"}},
		{"DFC", 62, 75, []string{"DFC"}},
		{"DFC", 38, 75, []string{"DFC"}},
		{"LI", 15, 70, []string{"LI"}},
	}
}

// Formaciones que usa el cuerpo técnico (fútbol 11).
var formations = []Formation{
	{"4-4-2", append(back4(),
		Slot{"VD", 85, 46, []string{"VD"}}, Slot{"MC", 62, 52, []string{"MC", "ENG"}},
		Slot{"MC", 38, 52, []string{"MC", "ENG"}}, Slot{"VI", 15, 46, []string{"VI"}},
		Slot{"DC", 62, 20, []string{"DC"}}, Slot{"DC", 38, 20, []string{"DC"}})},
	{"4-3-3", append(back4(),
		Slot{"MC", 72, 50, []string{"MC", "ENG"}}, Slot{"MC", 50, 56, []string{"MC"}},
		Slot{"MC", 28, 50, []string{"MC", "ENG"}}, Slot{"VD", 84, 24, []string{"VD"}},
		Slot{"DC", 50, 16, []string{"DC"}}, Slot{"VI", 16, 24, []string{"VI"}})},
	{"4-2-3-1", append(back4(),
		Slot{"MC", 62, 57, []string{"MC"}}, Slot{"MC", 38, 57, []string{"MC"}},
		Slot{"VD", 84, 34, []string{"VD"}}, Slot{"ENG", 50, 36, []string{"ENG", "MC"}},
		Slot{"VI", 16, 34, []string{"VI"}}, Slot{"DC", 50, 14, []string{"DC"}})},
	{"3-5-2", []Slot{
		{"POR", 50, 90, []string{"POR"}},
		{"DFC", 72, 74, []string{"DFC", "LD"}}, {"DFC", 50, 77, []string{"DFC"}}, {"DFC", 28, 74, []string{"DFC", "LI"}},
		{"VD", 88, 46, []string{"VD", "LD"}}, {"MC", 64, 56, []string{"MC"}},
		{"MC", 36, 56, []string{"MC"}}, {"ENG", 50, 38, []string{"ENG", "MC"}},
		{"VI", 12, 46, []string{"VI", "LI"}},
		{"DC", 62, 20, []string{"DC"}}, {"DC", 38, 20, []string{"DC"}},
	}},
}

func formationByName(name string) (Formation, bool) {
	for _, f := range formations {
		if f.Name == name {
			return f, true
		}
	}
	return Formation{}, false
}

// fits: el jugador tiene alguna posición que encaja en el puesto.
func (s Slot) fits(p Player) bool {
	for _, c := range s.Fits {
		if p.HasPosition(c) {
			return true
		}
	}
	return false
}

// LineupSlot: puesto con su jugador (si lo hay) y los convocados ordenados
// para elegir: primero los que encajan en el puesto.
type LineupSlot struct {
	Index    int
	Slot     Slot
	Player   *Player
	Suggest  []Player
	Others   []Player
	Selected int64
}

// buildLineup cruza formación, convocados y asignaciones guardadas.
func buildLineup(f Formation, in []Player, assigned map[int]int64) ([]LineupSlot, []Player) {
	byID := map[int64]Player{}
	for _, p := range in {
		byID[p.ID] = p
	}
	used := map[int64]bool{}
	out := make([]LineupSlot, len(f.Slots))
	for i, s := range f.Slots {
		ls := LineupSlot{Index: i, Slot: s}
		if pid, ok := assigned[i]; ok {
			if p, ok := byID[pid]; ok {
				ls.Player, ls.Selected = &p, pid
				used[pid] = true
			}
		}
		for _, p := range in {
			if s.fits(p) {
				ls.Suggest = append(ls.Suggest, p)
			} else {
				ls.Others = append(ls.Others, p)
			}
		}
		out[i] = ls
	}
	var bench []Player
	for _, p := range in {
		if !used[p.ID] {
			bench = append(bench, p)
		}
	}
	return out, bench
}

func (s store) lineup(ctx context.Context, callupID int64) (map[int]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT slot, player_id FROM callup_lineup WHERE callup_id = $1", callupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]int64{}
	for rows.Next() {
		var slot int
		var pid int64
		if err := rows.Scan(&slot, &pid); err != nil {
			return nil, err
		}
		out[slot] = pid
	}
	return out, rows.Err()
}

var errDuplicatePlayer = errors.New("un jugador no puede ocupar dos puestos")

// saveLineup guarda formación y puestos. Cambiar de formación vacía los
// puestos: no se corresponden entre formaciones.
func (s store) saveLineup(ctx context.Context, teamID, callupID int64, formation string, slots map[int]int64) error {
	seen := map[int64]bool{}
	for _, pid := range slots {
		if seen[pid] {
			return errDuplicatePlayer
		}
		seen[pid] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var prev sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT formation FROM callups WHERE team_id = $1 AND id = $2 FOR UPDATE",
		teamID, callupID).Scan(&prev); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE callups SET formation = $2 WHERE id = $1", callupID, formation); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM callup_lineup WHERE callup_id = $1", callupID); err != nil {
		return err
	}
	if prev.String == formation {
		for slot, pid := range slots {
			// El FK compuesto rechaza a quien no esté convocado.
			if _, err := tx.ExecContext(ctx, "INSERT INTO callup_lineup (callup_id, slot, player_id) VALUES ($1, $2, $3)",
				callupID, slot, pid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
