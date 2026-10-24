package main

import (
	"bytes"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestReportPDF(t *testing.T) {
	dia := func(d int) time.Time { return time.Date(2026, 10, d, 20, 0, 0, 0, time.UTC) }
	ps := []Player{
		{ID: 1, FirstName: "Juan", LastName: "Pérez", Nickname: "Juanito", Number: sql.NullInt64{Int64: 9, Valid: true}, Active: true,
			Attended: 3, Trainings: 4, Goals: 5, CycleYellows: 4, Streak: 2,
			Cycle: []Mark{{dia(13), Presente}, {dia(15), Tarde}, {dia(20), Injustificado}, {dia(22), ""}}},
		{ID: 2, FirstName: "Íñigo", LastName: "Muñoz Ñandú", Active: true, Attended: 1, Trainings: 4, Reds: 1, Doubles: 1,
			Suspension: "roja directa", PendingGames: 2, Late: lateAlert,
			Cycle: []Mark{{dia(13), Lesionado}, {dia(15), Justificado}, {dia(20), Presente}, {dia(22), Presente}}},
		{ID: 3, FirstName: "Baja", Active: false},
	}
	d := reportData{
		Team:      Team{ID: 1, Name: "Cómpeta C.F. Juvenil"},
		Players:   ps,
		Trainings: []Training{{Day: dia(13)}, {Day: dia(15)}, {Day: dia(20)}, {Day: dia(22)}},
		Cycle:     cycleInfo{Since: dia(12), FromMatch: true, Trainings: 4},
		Scorers:   []Scorer{{Player: ps[0], Goals: 5}},
		Booked:    booked(ps),
		Sent:      sentOff(ps),
		Now:       dia(24),
	}
	var b bytes.Buffer
	if err := writeReportPDF(&b, d); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b.Bytes(), []byte("%PDF-")) || b.Len() < 1000 {
		t.Fatalf("no parece un PDF (%d bytes)", b.Len())
	}
	// Para revisarlo a ojo: REPORT_PDF=/tmp/x.pdf go test -run TestReportPDF
	if f := os.Getenv("REPORT_PDF"); f != "" {
		if err := os.WriteFile(f, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := slug("Cómpeta C.F. Juvenil "); got != "competa-c-f-juvenil" {
		t.Fatalf("slug = %q", got)
	}
}
