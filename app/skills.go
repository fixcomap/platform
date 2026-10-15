package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Position: las posiciones que usa el cuerpo técnico.
type Position struct {
	Code, Name string
}

var positions = []Position{
	{"POR", "Portero"}, {"DFC", "Defensa central"}, {"LI", "Lateral izquierdo"}, {"LD", "Lateral derecho"},
	{"MC", "Mediocampista"}, {"ENG", "Enganche"}, {"VI", "Volante izquierdo"}, {"VD", "Volante derecho"},
	{"DC", "Delantero"},
}

func validPosition(code string) bool {
	for _, p := range positions {
		if p.Code == code {
			return true
		}
	}
	return false
}

// PositionList: posiciones del jugador, la primera es la principal.
func (p Player) PositionList() []string {
	if p.Positions == "" {
		return nil
	}
	return strings.Split(p.Positions, ",")
}

func (p Player) HasPosition(code string) bool {
	for _, c := range p.PositionList() {
		if c == code {
			return true
		}
	}
	return false
}

// Skill: una cualidad puntuable. Key es la columna en player_skills.
type Skill struct {
	Key, Name, Short string
}

// Orden del hexágono (como la carta del FIFA): VEL, TIR, PAS, REG, DEF, FIS.
var skills = []Skill{
	{"pace", "Velocidad", "VEL"}, {"shooting", "Tiro", "TIR"}, {"passing", "Pase", "PAS"},
	{"dribbling", "Regate", "REG"}, {"defending", "Defensa", "DEF"}, {"physical", "Físico", "FIS"},
}

// Skills: notas por clave; las que faltan no se han valorado.
type Skills map[string]float64

// Overall: media de las cualidades valoradas, redondeada a 0,5.
func (s Skills) Overall() float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += v
	}
	return math.Round(sum/float64(len(s))*2) / 2
}

// fmtScore: 7 → "7", 7.5 → "7,5".
func fmtScore(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1)
}

// hexPoints: vértices del polígono del hexágono (estilo PES) en un viewBox de
// 200x200 con centro (100,100) y radio 80 para la nota 10. scale=1 dibuja el
// valor de cada cualidad; las guías usan valores fijos.
func hexPoints(values []float64) string {
	var b strings.Builder
	for i, v := range values {
		ang := -math.Pi/2 + float64(i)*math.Pi/3
		r := 80 * v / 10
		fmt.Fprintf(&b, "%.1f,%.1f ", 100+r*math.Cos(ang), 100+r*math.Sin(ang))
	}
	return strings.TrimSpace(b.String())
}

// skillValues en el orden de skills; 0 si no está valorada.
func (s Skills) values() []float64 {
	out := make([]float64, len(skills))
	for i, k := range skills {
		out[i] = s[k.Key]
	}
	return out
}

func (s Skills) Hex() string { return hexPoints(s.values()) }

// hexGuide: hexágono de referencia a una nota fija (anillos 2, 4, 6, 8, 10).
func hexGuide(level float64) string {
	v := make([]float64, len(skills))
	for i := range v {
		v[i] = level
	}
	return hexPoints(v)
}

// hexLabel: posición del rótulo de la cualidad i, algo fuera del hexágono.
func hexLabel(i int) [2]float64 {
	ang := -math.Pi/2 + float64(i)*math.Pi/3
	return [2]float64{100 + 94*math.Cos(ang), 100 + 94*math.Sin(ang)}
}

func parseScore(s string) (float64, bool, error) {
	s = strings.TrimSpace(strings.Replace(s, ",", ".", 1))
	if s == "" {
		return 0, false, nil
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil || v < 1 || v > 10 || v*2 != math.Trunc(v*2) {
		return 0, false, errors.New("nota inválida: de 1 a 10 en pasos de 0,5")
	}
	return v, true, nil
}

func (s store) skills(ctx context.Context, playerID int64) (Skills, error) {
	vals := make([]sql.NullFloat64, len(skills))
	dst := make([]any, len(skills))
	cols := make([]string, len(skills))
	for i, k := range skills {
		dst[i] = &vals[i]
		cols[i] = k.Key + "::float8"
	}
	err := s.db.QueryRowContext(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM player_skills WHERE player_id = $1",
		playerID).Scan(dst...)
	out := Skills{}
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for i, k := range skills {
		if vals[i].Valid {
			out[k.Key] = vals[i].Float64
		}
	}
	return out, nil
}

// saveSkills reemplaza las notas del jugador (solo si es del equipo).
func (s store) saveSkills(ctx context.Context, teamID, playerID int64, sk Skills) error {
	cols := make([]string, len(skills))
	args := []any{teamID, playerID}
	sets := make([]string, len(skills))
	for i, k := range skills {
		cols[i] = k.Key
		if v, ok := sk[k.Key]; ok {
			args = append(args, v)
		} else {
			args = append(args, nil)
		}
		sets[i] = fmt.Sprintf("%s = EXCLUDED.%s", k.Key, k.Key)
	}
	ph := make([]string, len(skills))
	for i := range ph {
		ph[i] = fmt.Sprintf("$%d", i+3)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO player_skills (player_id, `+strings.Join(cols, ", ")+`)
		SELECT id, `+strings.Join(ph, ", ")+` FROM players WHERE team_id = $1 AND id = $2
		ON CONFLICT (player_id) DO UPDATE SET `+strings.Join(sets, ", ")+`, updated_at = now()`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
