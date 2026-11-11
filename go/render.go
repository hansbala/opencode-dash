package main

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type theme struct {
	enabled bool
}

func (t theme) paint(code, text string) string {
	if !t.enabled {
		return text
	}
	return code + text + "\x1b[0m"
}

func (t theme) bold(text string) string  { return t.paint("\x1b[1m", text) }
func (t theme) white(text string) string { return t.paint("\x1b[38;2;235;237;241m", text) }
func (t theme) gray(text string) string  { return t.paint("\x1b[38;2;138;143;152m", text) }
func (t theme) dim(text string) string   { return t.paint("\x1b[38;2;92;97;107m", text) }
func (t theme) green(text string) string { return t.paint("\x1b[38;2;52;199;115m", text) }
func (t theme) yellow(text string) string {
	return t.paint("\x1b[38;2;250;191;64m", text)
}
func (t theme) red(text string) string { return t.paint("\x1b[38;2;242;85;90m", text) }

func (t theme) percentColor(percent float64) string {
	if !t.enabled {
		return ""
	}
	switch {
	case percent >= 90:
		return "\x1b[38;2;242;85;90m"
	case percent >= 70:
		return "\x1b[38;2;250;191;64m"
	default:
		return "\x1b[38;2;52;199;115m"
	}
}

func (t theme) coloredPercent(percent float64, text string) string {
	switch {
	case percent >= 90:
		return t.red(text)
	case percent >= 70:
		return t.yellow(text)
	default:
		return t.green(text)
	}
}

func visibleLength(text string) int {
	count := 0
	escaping := false
	for _, r := range text {
		if escaping {
			if r == 'm' {
				escaping = false
			}
			continue
		}
		if r == '\x1b' {
			escaping = true
			continue
		}
		count++
	}
	return count
}

func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-visibleLength(text)))
}

func padLeft(text string, width int) string {
	return strings.Repeat(" ", max(0, width-visibleLength(text))) + text
}

func padBetween(left, right string, width int) string {
	return left + strings.Repeat(" ", max(1, width-visibleLength(left)-visibleLength(right))) + right
}

func truncate(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	if width <= 1 {
		return string(runes[:max(0, width)])
	}
	return string(runes[:width-1]) + "…"
}

func truncateVisible(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleLength(text) <= width {
		return text
	}
	var out strings.Builder
	visible := 0
	escaping := false
	hadEscape := false
	for _, r := range text {
		if escaping {
			out.WriteRune(r)
			if r == 'm' {
				escaping = false
			}
			continue
		}
		if r == '\x1b' {
			escaping = true
			hadEscape = true
			out.WriteRune(r)
			continue
		}
		if visible >= width {
			break
		}
		out.WriteRune(r)
		visible++
	}
	if hadEscape {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

func bar(fraction float64, width int, t theme) string {
	fraction = min(1, max(0, fraction))
	filled := int(math.Round(fraction * float64(width)))
	return t.percentColor(fraction*100) +
		strings.Repeat("█", filled) +
		t.dim(strings.Repeat("░", max(0, width-filled)))
}

const (
	seriesHistory = iota
	seriesForecast
	seriesGrid
)

type brailleCanvas struct {
	cols     int
	rows     int
	history  []uint8
	forecast []uint8
	grid     []uint8
}

func newBrailleCanvas(cols, rows int) *brailleCanvas {
	size := cols * rows
	return &brailleCanvas{
		cols:     cols,
		rows:     rows,
		history:  make([]uint8, size),
		forecast: make([]uint8, size),
		grid:     make([]uint8, size),
	}
}

func (c *brailleCanvas) pixelWidth() int  { return c.cols * 2 }
func (c *brailleCanvas) pixelHeight() int { return c.rows * 4 }

func (c *brailleCanvas) plot(x, y, series int) {
	if x < 0 || y < 0 || x >= c.pixelWidth() || y >= c.pixelHeight() {
		return
	}
	index := (y/4)*c.cols + x/2
	var bit uint8
	switch x % 2 {
	case 0:
		switch y % 4 {
		case 0:
			bit = 0x01
		case 1:
			bit = 0x02
		case 2:
			bit = 0x04
		case 3:
			bit = 0x40
		}
	case 1:
		switch y % 4 {
		case 0:
			bit = 0x08
		case 1:
			bit = 0x10
		case 2:
			bit = 0x20
		case 3:
			bit = 0x80
		}
	}
	if bit == 0 {
		return
	}
	switch series {
	case seriesHistory:
		c.history[index] |= bit
	case seriesForecast:
		c.forecast[index] |= bit
	case seriesGrid:
		c.grid[index] |= bit
	}
}

func (c *brailleCanvas) line(x0, y0, x1, y1 float64, series int, dashed bool) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0)))
	if steps < 1 {
		steps = 1
	}
	for step := 0; step <= steps; step++ {
		if dashed && (step/2)%2 == 0 {
			continue
		}
		t := float64(step) / float64(steps)
		c.plot(
			int(math.Round(x0+(x1-x0)*t)),
			int(math.Round(y0+(y1-y0)*t)),
			series,
		)
	}
}

func (c *brailleCanvas) render(t theme) []string {
	lines := make([]string, c.rows)
	for row := 0; row < c.rows; row++ {
		var line strings.Builder
		for col := 0; col < c.cols; col++ {
			index := row*c.cols + col
			switch {
			case c.history[index] != 0:
				line.WriteString(t.green(braille(c.history[index])))
			case c.forecast[index] != 0:
				line.WriteString(t.yellow(braille(c.forecast[index])))
			case c.grid[index] != 0:
				line.WriteString(t.dim(braille(c.grid[index])))
			default:
				line.WriteByte(' ')
			}
		}
		lines[row] = line.String()
	}
	return lines
}

func braille(bits uint8) string {
	return string(rune(0x2800 + int(bits)))
}

func render(
	snapshot *snapshot,
	sessions []sessionSummary,
	showSessions bool,
	width, height int,
	color bool,
	errMessage string,
) string {
	t := theme{enabled: color}
	lines := make([]string, 0, height)

	workspace := "OpenCode"
	updated := "—"
	if snapshot != nil {
		if snapshot.workspace != "" {
			workspace = snapshot.workspace
		}
		updated = snapshot.fetchedAt.Format("3:04 PM")
	}
	header := "  " + t.bold(t.white("OPENCODE DASH")) + t.dim("  ·  ") + t.gray(workspace)
	lines = append(lines, padBetween(header, t.dim("updated "+updated+"  "), width))
	lines = append(lines, t.dim(strings.Repeat("─", max(0, width))))

	if snapshot == nil {
		lines = append(lines, "")
		if errMessage != "" {
			lines = append(lines, "  "+t.red(errMessage))
		} else {
			lines = append(lines, "  "+t.gray("Connecting to the local OpenCode service…"))
		}
		lines = append(lines, "")
		lines = append(lines, "  "+t.dim("q quit   r retry"))
		return strings.Join(lines, "\n")
	}

	if showSessions {
		renderSessions(sessions, width, height, t, &lines)
	} else {
		renderDashboard(snapshot, width, height, t, &lines)
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = truncateVisible(lines[index], width)
	}
	return strings.Join(lines, "\n")
}

func renderDashboard(s *snapshot, width, height int, t theme, lines *[]string) {
	modelCount := 1
	switch {
	case height >= 30:
		modelCount = 3
	case height >= 26:
		modelCount = 2
	}
	chartHeight := min(10, max(4, height-19-modelCount))

	percent := s.percent()
	subtitle := "Renewal date unavailable"
	if reset := s.resetDate(); !reset.IsZero() {
		subtitle = "Renews " + shortDate(reset)
	}
	if pace, ok := s.pace(); ok {
		subtitle += fmt.Sprintf("  ·  on pace for %.0f%%", pace)
	}
	if average, ok := s.dailyAverage(); ok {
		subtitle += fmt.Sprintf("  ·  %.1f%%/day", average)
	}

	*lines = append(*lines, "")
	*lines = append(*lines, "  "+t.dim("GO USAGE"))
	*lines = append(*lines,
		"  "+t.bold(t.coloredPercent(percent, fmt.Sprintf("%.0f%%", percent)))+"  "+t.gray(subtitle))

	renderChart(s, width, chartHeight, t, lines)

	*lines = append(*lines, "")
	*lines = append(*lines, "  "+t.bold("LIMITS"))
	if s.goInfo != nil {
		barWidth := max(12, min(52, width-40))
		renderLimit("Rolling", s.goInfo.Rolling, barWidth, width, t, lines)
		renderLimit("Weekly", s.goInfo.Weekly, barWidth, width, t, lines)
		renderLimit("Monthly", s.goInfo.Monthly, barWidth, width, t, lines)
	} else {
		*lines = append(*lines, "  "+t.gray("No limit data available"))
	}

	*lines = append(*lines, "")
	meta := fmt.Sprintf("%d sessions · %s steps  ", s.stats.Sessions, compactCount(s.stats.Steps))
	*lines = append(*lines, padBetween("  "+t.bold("MODELS"), t.dim(meta), width))

	models := s.modelsByUsage()
	if len(models) > modelCount {
		models = models[:modelCount]
	}
	modelBarWidth := max(10, min(36, width-40))
	for _, usage := range models {
		share := s.share(usage)
		name := pad(truncate(usage.Model.ID, 22), 22)
		percentText := padLeft(fmt.Sprintf("%.0f%%", share), 4)
		filled := int(math.Round(share / 100 * float64(modelBarWidth)))
		modelBar := t.green(strings.Repeat("█", filled)) +
			t.dim(strings.Repeat("░", max(0, modelBarWidth-filled)))
		left := "  " + t.gray(name) + " " + t.white(percentText) + "  " + modelBar
		cost := t.dim(fmt.Sprintf("$%.2f", usage.Cost)) + "  "
		*lines = append(*lines, padBetween(left, cost, width))
	}
	if len(models) == 0 {
		*lines = append(*lines, "  "+t.dim("No model activity this cycle"))
	}

	*lines = append(*lines, "")
	*lines = append(*lines,
		"  "+t.dim("tokens ")+t.white(compactCount(s.stats.Tokens.Total()))+
			"    "+t.dim("cost ")+t.white(fmt.Sprintf("$%.2f", s.stats.Cost))+
			"    "+t.dim("prompts ")+t.white(fmt.Sprintf("%d", s.stats.Prompts))+
			"    "+t.dim("steps ")+t.white(compactCount(s.stats.Steps)))

	*lines = append(*lines, "")
	*lines = append(*lines, "  "+t.dim("q quit   r refresh   s sessions   o open dashboard"))
}

func renderLimit(name string, window limitWindow, barWidth, width int, t theme, lines *[]string) {
	left := "  " + t.gray(pad(name, 8)) + " " +
		t.white(padLeft(fmt.Sprintf("%.0f%%", window.Percent), 4)) + "  " +
		bar(window.Percent/100, barWidth, t)
	right := t.dim("resets in " + countdown(window.resetDate()))
	*lines = append(*lines, padBetween(left, right, width))
}

func renderChart(s *snapshot, width, height int, t theme, lines *[]string) {
	chartWidth := max(10, width-8)
	canvas := newBrailleCanvas(chartWidth, height)
	const maxPercent = 150.0

	x := func(day float64) float64 {
		return min(1, max(0, day/30)) * float64(canvas.pixelWidth()-1)
	}
	y := func(percent float64) float64 {
		return (1 - min(maxPercent, max(0, percent))/maxPercent) * float64(canvas.pixelHeight()-1)
	}

	for _, percent := range []float64{0, 50, 100, 150} {
		py := int(math.Round(y(percent)))
		for px := 0; px < canvas.pixelWidth(); px += 6 {
			canvas.plot(px, py, seriesGrid)
		}
	}

	points := s.chartPoints()
	for index := 0; index < len(points)-1; index++ {
		start, end := points[index], points[index+1]
		canvas.line(x(start.day), y(start.percent), x(end.day), y(end.percent), seriesHistory, false)
	}
	if pace, ok := s.pace(); ok && len(points) > 0 {
		last := points[len(points)-1]
		canvas.line(x(last.day), y(last.percent), x(30), y(pace), seriesForecast, true)
	}

	chartLines := canvas.render(t)

	labelRow := func(percent float64) int {
		return int(math.Round((1 - percent/maxPercent) * float64(height-1)))
	}
	labels := map[int]string{}
	for _, percent := range []float64{0, 50, 100, 150} {
		labels[labelRow(percent)] = fmt.Sprintf("%4.0f%%", percent)
	}

	for row := 0; row < height; row++ {
		*lines = append(*lines, t.dim(padLeft(labels[row], 5))+" "+t.dim("│")+chartLines[row])
	}

	*lines = append(*lines, "       "+t.dim("└"+strings.Repeat("─", chartWidth)))

	start := "START"
	if cycleStart := s.cycleStart(); !cycleStart.IsZero() {
		start = strings.ToUpper(shortDate(cycleStart))
	}
	end := "RENEWAL"
	if reset := s.resetDate(); !reset.IsZero() {
		end = strings.ToUpper(shortDate(reset))
	}
	*lines = append(*lines, padBetween("       "+t.dim(start), t.dim(end)+"  ", width))

	*lines = append(*lines, "       "+t.green("●")+t.gray(" usage   ")+
		t.yellow("╱")+t.gray(" forecast   ")+t.dim("┄ 100% limit"))
}

func renderSessions(sessions []sessionSummary, width, height int, t theme, lines *[]string) {
	*lines = append(*lines, "")
	*lines = append(*lines, padBetween("  "+t.bold("RECENT SESSIONS"), t.dim("s back  "), width))
	*lines = append(*lines,
		"  "+t.dim(pad("ago", 8)+pad("model", 24)+pad("cost", 9)+"  title"))

	available := max(1, height-6)
	if len(sessions) == 0 {
		*lines = append(*lines, "  "+t.dim("No sessions yet"))
	} else {
		if len(sessions) > available {
			sessions = sessions[:available]
		}
		for _, session := range sessions {
			created := time.UnixMilli(session.Time.Created)
			model := "—"
			if session.Model != nil {
				model = session.Model.ID
			}
			title := truncate(session.TitleOrID(), max(10, width-45))
			*lines = append(*lines,
				"  "+t.gray(pad(relativeTime(created), 8))+
					t.white(pad(truncate(model, 22), 24))+
					t.gray(padLeft(fmt.Sprintf("$%.2f", session.Cost), 9))+
					"  "+t.dim(title))
		}
	}

	*lines = append(*lines, "")
	*lines = append(*lines, "  "+t.dim("q quit   r refresh   s dashboard   o open dashboard"))
}

func (s sessionSummary) TitleOrID() string {
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}
