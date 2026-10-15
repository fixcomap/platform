package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Estado de una pizarra. Coordenadas en metros sobre un campo vertical de
// 68 x 105 (portería propia abajo), el mismo sistema que dibuja board.js.
type BoardToken struct {
	Label string  `json:"label"` // texto en la ficha (posición o dorsal)
	Name  string  `json:"name"`  // nombre bajo la ficha
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

type BoardPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type BoardArrow struct {
	From BoardPoint `json:"from"`
	To   BoardPoint `json:"to"`
}

type BoardState struct {
	Tokens []BoardToken `json:"tokens"`
	Ball   BoardPoint   `json:"ball"`
	Arrows []BoardArrow `json:"arrows"`
}

const (
	pitchW, pitchL = 68.0, 105.0
	maxTokens      = 30
	maxArrows      = 60
	maxLabel       = 24
)

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

func (p *BoardPoint) clamp() { p.X, p.Y = clamp(p.X, 0, pitchW), clamp(p.Y, 0, pitchL) }

// sanitize acota lo que llega del navegador: número de elementos, textos y
// coordenadas dentro del campo.
func (s *BoardState) sanitize() error {
	if len(s.Tokens) > maxTokens || len(s.Arrows) > maxArrows {
		return errors.New("demasiados elementos en la pizarra")
	}
	for i := range s.Tokens {
		t := &s.Tokens[i]
		t.X, t.Y = clamp(t.X, 0, pitchW), clamp(t.Y, 0, pitchL)
		t.Label, t.Name = cut(strings.TrimSpace(t.Label)), cut(strings.TrimSpace(t.Name))
	}
	s.Ball.clamp()
	for i := range s.Arrows {
		s.Arrows[i].From.clamp()
		s.Arrows[i].To.clamp()
	}
	if s.Tokens == nil {
		s.Tokens = []BoardToken{}
	}
	if s.Arrows == nil {
		s.Arrows = []BoardArrow{}
	}
	return nil
}

func cut(s string) string {
	for utf8.RuneCountInString(s) > maxLabel {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// stateFromFormation coloca las fichas en los puestos de una formación; si
// hay planteamiento (assigned) pone el nombre de cada jugador.
func stateFromFormation(f Formation, names map[int]string) BoardState {
	st := BoardState{Ball: BoardPoint{pitchW / 2, pitchL / 2}}
	for i, sl := range f.Slots {
		st.Tokens = append(st.Tokens, BoardToken{
			Label: sl.Code, Name: names[i],
			X: sl.X / 100 * pitchW, Y: sl.Y / 100 * pitchL,
		})
	}
	_ = st.sanitize()
	return st
}

type Board struct {
	ID        int64
	Name      string
	State     BoardState
	UpdatedAt time.Time
}

func (s store) boards(ctx context.Context, teamID int64) ([]Board, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, updated_at FROM boards
		WHERE team_id = $1 ORDER BY updated_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s store) board(ctx context.Context, teamID, id int64) (Board, error) {
	b := Board{ID: id}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, "SELECT name, state, updated_at FROM boards WHERE team_id = $1 AND id = $2",
		teamID, id).Scan(&b.Name, &raw, &b.UpdatedAt); err != nil {
		return b, err
	}
	if err := json.Unmarshal(raw, &b.State); err != nil {
		return b, err
	}
	return b, b.State.sanitize()
}

func (s store) createBoard(ctx context.Context, teamID int64, name string, st BoardState) (int64, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, "INSERT INTO boards (team_id, name, state) VALUES ($1, $2, $3) RETURNING id",
		teamID, name, raw).Scan(&id)
	return id, err
}

func (s store) saveBoard(ctx context.Context, teamID, id int64, name string, st BoardState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE boards SET name = $3, state = $4, updated_at = now()
		WHERE team_id = $1 AND id = $2`, teamID, id, name, raw)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s store) deleteBoard(ctx context.Context, teamID, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM boards WHERE team_id = $1 AND id = $2", teamID, id)
	return err
}
