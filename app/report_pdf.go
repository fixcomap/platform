package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// El informe de jugadores como PDF de verdad, generado aquí. La versión HTML
// (/players/report) depende de window.print(), que iOS ignora en la app
// instalada en la pantalla de inicio: este endpoint funciona en cualquier sitio.
// Fuentes estándar del PDF (sin ficheros ni dependencias de sistema); no traen
// ✓ ni ✗, así que en la tabla van V y X, con la leyenda al pie.

type reportData struct {
	Team      Team
	Players   []Player
	Trainings []Training
	Cycle     cycleInfo
	Scorers   []Scorer
	Booked    []Player
	Sent      []Player
	Now       time.Time
}

func (a *app) playersReportPDF(w http.ResponseWriter, r *http.Request, t Team) {
	data, err := a.playersData(r, t)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	d := reportData{
		Team:      t,
		Players:   data["Players"].([]Player),
		Trainings: data["Trainings"].([]Training),
		Cycle:     data["Cycle"].(cycleInfo),
		Scorers:   data["Scorers"].([]Scorer),
		Booked:    data["Booked"].([]Player),
		Sent:      data["Sent"].([]Player),
		Now:       data["Now"].(time.Time),
	}
	nombre := fmt.Sprintf("informe-%s-%s.pdf", slug(t.Name), d.Now.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	// inline: en el iPhone se abre el visor con el botón de compartir (WhatsApp,
	// Archivos); en el ordenador, el visor del navegador con su descarga.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, nombre))
	w.Header().Set("Cache-Control", "no-store")
	if err := writeReportPDF(w, d); err != nil {
		a.fail(w, r, err)
	}
}

var (
	violeta   = [3]int{59, 47, 143}
	gris      = [3]int{107, 104, 128}
	estadoPDF = map[Status]struct {
		texto string
		color [3]int
	}{
		Presente:      {"V", [3]int{31, 138, 76}},
		Tarde:         {"T", violeta},
		Lesionado:     {"L", [3]int{154, 91, 0}},
		Justificado:   {"J", [3]int{37, 87, 167}},
		Injustificado: {"X", [3]int{179, 38, 30}},
	}
	diasPDF = [...]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
)

func writeReportPDF(out io.Writer, d reportData) error {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(true, 10)
	tr := pdf.UnicodeTranslatorFromDescriptor("") // cp1252: tildes, ñ, ¿¡
	pdf.SetTitle(tr(d.Team.Name+" · Informe de jugadores"), false)
	pdf.SetCreator("plantilla.fixcomap.com", false)
	color := func(c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }

	// Columnas: fijas + una por entreno del ciclo + avisos con el resto del ancho.
	const ancho = 277.0
	dia := 9.0
	if n := float64(len(d.Trainings)); n > 0 && n*dia > 100 {
		dia = 100 / n
	}
	fijas := []float64{9, 58}
	finales := []float64{25, 13, 17, 20}
	avisos := ancho - sum(fijas) - dia*float64(len(d.Trainings)) - sum(finales)

	cabecera := func() {
		pdf.SetFont("Helvetica", "B", 7.5)
		color(gris)
		pdf.SetFillColor(255, 255, 255)
		pdf.CellFormat(fijas[0], 6, tr("Nº"), "B", 0, "C", false, 0, "")
		pdf.CellFormat(fijas[1], 6, tr("Jugador"), "B", 0, "L", false, 0, "")
		for _, t := range d.Trainings {
			pdf.CellFormat(dia, 6, tr(fmt.Sprintf("%s %d", diasPDF[t.Day.Weekday()], t.Day.Day())), "B", 0, "C", false, 0, "")
		}
		for i, h := range []string{"Asistencia", "Goles", "Amarillas", "Rojas"} {
			pdf.CellFormat(finales[i], 6, h, "B", 0, "C", false, 0, "")
		}
		pdf.CellFormat(avisos, 6, "Avisos", "B", 1, "L", false, 0, "")
	}
	pdf.SetHeaderFuncMode(func() {
		if pdf.PageNo() > 1 {
			cabecera()
		}
	}, true)

	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 15)
	color(violeta)
	pdf.CellFormat(200, 8, tr(d.Team.Name+" · Informe de jugadores"), "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	color(gris)
	pdf.CellFormat(77, 8, tr(diasPDF[d.Now.Weekday()]+" "+d.Now.Format("02/01/2006")), "", 1, "R", false, 0, "")
	periodo := "Entrenos de los últimos 14 días"
	if d.Cycle.FromMatch {
		periodo = "Entrenos desde el partido del " + d.Cycle.Since.Format("02/01/2006")
	}
	pdf.SetFont("Helvetica", "", 8)
	pdf.MultiCell(ancho, 4, tr(periodo+" · Asistencia, goles y tarjetas: temporada desde el 1 de agosto (tarjetas y goles solo en partidos oficiales)."), "", "L", false)
	pdf.Ln(2)
	cabecera()

	par := false
	for _, p := range d.Players {
		if !p.Active {
			continue
		}
		par = !par
		if par {
			pdf.SetFillColor(247, 246, 252)
		} else {
			pdf.SetFillColor(255, 255, 255)
		}
		const alto = 6.0
		pdf.SetFont("Helvetica", "", 8.5)
		color([3]int{29, 26, 46})
		numero := ""
		if p.Number.Valid {
			numero = fmt.Sprint(p.Number.Int64)
		}
		pdf.CellFormat(fijas[0], alto, numero, "", 0, "C", true, 0, "")
		jugador := p.DisplayName()
		if p.Nickname != "" {
			jugador += " (" + strings.TrimSpace(p.FirstName+" "+p.LastName) + ")"
		}
		pdf.CellFormat(fijas[1], alto, tr(recortar(pdf, tr(jugador), fijas[1]-2)), "", 0, "L", true, 0, "")
		for _, m := range p.Cycle {
			if e, ok := estadoPDF[m.Status]; ok {
				pdf.SetFont("Helvetica", "B", 8.5)
				color(e.color)
				pdf.CellFormat(dia, alto, e.texto, "", 0, "C", true, 0, "")
				pdf.SetFont("Helvetica", "", 8.5)
				color([3]int{29, 26, 46})
			} else {
				color(gris)
				pdf.CellFormat(dia, alto, tr("·"), "", 0, "C", true, 0, "")
				color([3]int{29, 26, 46})
			}
		}
		asistencia := "—"
		if p.Trainings > 0 {
			asistencia = fmt.Sprintf("%d%% (%d/%d)", p.Percent(), p.Attended, p.Trainings)
		}
		pdf.CellFormat(finales[0], alto, tr(asistencia), "", 0, "C", true, 0, "")
		pdf.CellFormat(finales[1], alto, siNoCero(p.Goals), "", 0, "C", true, 0, "")
		pdf.CellFormat(finales[2], alto, siNoCero(p.CycleYellows), "", 0, "C", true, 0, "")
		rojas := siNoCero(p.Reds)
		if p.Doubles > 0 {
			if rojas != "" {
				rojas += " + "
			}
			rojas += fmt.Sprintf("%d×2A", p.Doubles)
		}
		pdf.CellFormat(finales[3], alto, tr(rojas), "", 0, "C", true, 0, "")
		pdf.SetFont("Helvetica", "B", 7.5)
		color([3]int{179, 38, 30})
		pdf.CellFormat(avisos, alto, tr(recortar(pdf, tr(avisosDe(p)), avisos-2)), "", 1, "L", true, 0, "")
	}

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 7.5)
	color(gris)
	pdf.MultiCell(ancho, 4, tr("V vino · T llegó tarde · L lesionado · J ausencia justificada · X ausencia no justificada · · sin lista · 2A = doble amarilla"), "", "L", false)
	pdf.Ln(3)

	// Tres columnas: goleadores, amonestados y sanciones.
	var goleadores, amonestados, sancionados []string
	for _, s := range d.Scorers {
		goleadores = append(goleadores, fmt.Sprintf("%s — %d", s.Player.DisplayName(), s.Goals))
	}
	for _, p := range d.Booked {
		l := fmt.Sprintf("%s — %d amarilla%s", p.DisplayName(), p.CycleYellows, plural(p.CycleYellows))
		if p.Suspension != "" {
			l += " (sancionado)"
		} else if p.CycleYellows%5 == 4 {
			l += " (apercibido)"
		}
		amonestados = append(amonestados, l)
	}
	for _, p := range d.Sent {
		l := p.DisplayName()
		if p.Reds > 0 {
			l += fmt.Sprintf(" — %d roja%s", p.Reds, plural(p.Reds))
		}
		if p.Doubles > 0 {
			l += fmt.Sprintf(" — %d doble%s amarilla%s", p.Doubles, plural(p.Doubles), plural(p.Doubles))
		}
		if p.PendingGames > 0 {
			l += fmt.Sprintf(" · le quedan %d partido%s", p.PendingGames, plural(p.PendingGames))
		}
		sancionados = append(sancionados, l)
	}
	columnas := []struct {
		titulo, vacio string
		lineas        []string
	}{
		{"Goleadores", "Sin goles todavía.", goleadores},
		{"Amonestados", "Sin amonestados.", amonestados},
		{"Expulsiones y sanciones", "Sin expulsiones.", sancionados},
	}
	col := ancho / 3
	y := pdf.GetY()
	if y > 170 {
		pdf.AddPage()
		y = pdf.GetY()
	}
	for i, c := range columnas {
		pdf.SetXY(10+float64(i)*col, y)
		pdf.SetFont("Helvetica", "B", 10)
		color(violeta)
		pdf.CellFormat(col-4, 6, c.titulo, "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 8.5)
		if len(c.lineas) == 0 {
			color(gris)
			pdf.CellFormat(col-4, 5, tr(c.vacio), "", 2, "L", false, 0, "")
			continue
		}
		color([3]int{29, 26, 46})
		for n, l := range c.lineas {
			pdf.CellFormat(col-4, 5, tr(recortar(pdf, tr(fmt.Sprintf("%d. %s", n+1, l)), col-6)), "", 2, "L", false, 0, "")
		}
	}
	return pdf.Output(out)
}

// Los mismos avisos que la tabla HTML, en texto.
func avisosDe(p Player) string {
	var a []string
	switch {
	case p.Suspension != "":
		s := "Sancionado · " + p.Suspension
		if p.PendingGames > 1 {
			s += fmt.Sprintf(" · %d partidos", p.PendingGames)
		}
		a = append(a, s)
	case p.CycleYellows%5 == 4:
		a = append(a, "Apercibido")
	}
	if p.Streak >= 2 {
		a = append(a, fmt.Sprintf("%d faltas seguidas", p.Streak))
	}
	if p.Late >= lateAlert {
		a = append(a, fmt.Sprintf("%d tardes en %d", p.Late, lateWindow))
	}
	return strings.Join(a, " · ")
}

// Recorta con "…" para que quepa en el ancho de la celda (texto ya en cp1252).
func recortar(pdf *fpdf.Fpdf, s string, ancho float64) string {
	if pdf.GetStringWidth(s) <= ancho {
		return deCP1252(s)
	}
	r := []byte(s)
	for len(r) > 0 && pdf.GetStringWidth(string(r)+"...") > ancho {
		r = r[:len(r)-1]
	}
	return deCP1252(string(r)) + "…"
}

// recortar mide en cp1252 (lo que dibuja fpdf) y devuelve UTF-8 para volver a
// pasar por tr: así las tildes no se cuentan doble.
func deCP1252(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteRune(cp1252[s[i]])
	}
	return b.String()
}

var cp1252 = func() [256]rune {
	var t [256]rune
	for i := range t {
		t[i] = rune(i)
	}
	for i, r := range []rune{'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8d, 'Ž', 0x8f,
		0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9d, 'ž', 'Ÿ'} {
		t[0x80+i] = r
	}
	return t
}()

func siNoCero(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}

// "Cómpeta C.F. Juvenil" -> "competa-c-f-juvenil"
func slug(s string) string {
	reemplazos := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")
	s = reemplazos.Replace(strings.ToLower(s))
	var b strings.Builder
	guion := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			guion = false
		} else if !guion && b.Len() > 0 {
			b.WriteByte('-')
			guion = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
