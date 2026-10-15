package main

import (
	"testing"
	"time"
)

func day(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }

func TestComputeDiscipline(t *testing.T) {
	const a, b, c = 1, 2, 3
	y := func(n int) cardRow { return cardRow{Yellow: n} }
	ms := []matchCards{
		{ID: 1, Day: day("2026-09-06"), Cards: map[int64]cardRow{a: y(1), b: y(2)}},
		{ID: 2, Day: day("2026-09-13"), Cards: map[int64]cardRow{a: y(1), c: {Red: true, RedGames: 2}}},
		{ID: 3, Day: day("2026-09-20"), Friendly: true, Cards: map[int64]cardRow{a: y(1)}}, // no cuenta
		{ID: 4, Day: day("2026-09-27"), Cards: map[int64]cardRow{a: y(1)}},
		{ID: 5, Day: day("2026-10-04"), Cards: map[int64]cardRow{a: y(1)}},
		{ID: 6, Day: day("2026-10-11"), Cards: map[int64]cardRow{a: y(1)}}, // 5ª de a
		{ID: 7, Day: day("2026-10-18")},
	}
	d := computeDiscipline(ms, day("2026-10-13"))
	if d.Suspended[2][b] != "doble amarilla" {
		t.Errorf("b sancionado en 2 por doble amarilla: %v", d.Suspended[2])
	}
	// c: roja de 2 partidos en el 2 → no juega 4 ni 5 (el amistoso 3 no cumple).
	if d.Suspended[4][c] == "" || d.Suspended[5][c] == "" || d.Suspended[6][c] != "" {
		t.Errorf("c roja 2 partidos: 4=%q 5=%q 6=%q", d.Suspended[4][c], d.Suspended[5][c], d.Suspended[6][c])
	}
	if d.Suspended[7][a] != "5ª amarilla" {
		t.Errorf("a sancionado en 7 por 5ª amarilla: %v", d.Suspended[7])
	}
	if d.YellowsBefore[6][a] != 4 {
		t.Errorf("a apercibido (4) antes del 6: %d", d.YellowsBefore[6][a])
	}
	if d.Yellows[b] != 0 {
		t.Errorf("la doble amarilla no suma al ciclo: %d", d.Yellows[b])
	}
	if d.Reds[c] != 1 || d.DoubleYellows[b] != 1 {
		t.Errorf("expulsiones: rojas=%v dobles=%v", d.Reds, d.DoubleYellows)
	}
	if len(d.Next) != 0 {
		t.Errorf("sin sanciones pendientes tras el 7: %v", d.Next)
	}
	// Nueva temporada: el ciclo se reinicia.
	ms = append(ms, matchCards{ID: 8, Day: day("2027-08-30"), Cards: map[int64]cardRow{a: y(1)}})
	if d := computeDiscipline(ms, day("2027-09-01")); d.Yellows[a] != 1 {
		t.Errorf("reinicio de temporada: %d", d.Yellows[a])
	}
}
