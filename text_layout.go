package pdf

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// TextLayout is positioned page text grouped into reading lines.
type TextLayout struct {
	Lines []TextLine
}

// TextLine is one rendered text line or line fragment.
type TextLine struct {
	X        float64
	Y        float64
	FontSize float64
	Text     string
}

type layoutGlyph struct {
	text string
	x    float64
	y    float64
	w    float64
	size float64
}

type layoutRow struct {
	y      float64
	glyphs []layoutGlyph
	lines  []TextLine
}

// TextLayout returns page text with positions reconstructed into reading order.
func (p Page) TextLayout() (layout TextLayout, err error) {
	defer func() {
		if r := recover(); r != nil {
			layout = TextLayout{}
			err = errors.New(fmt.Sprint(r))
		}
	}()

	glyphs := pageGlyphs(p.Content())
	if len(glyphs) == 0 {
		return TextLayout{}, nil
	}

	rows := groupGlyphRows(glyphs)
	rows = splitRowsIntoLines(rows)
	layout.Lines = orderLayoutLines(rows)
	return layout, nil
}

// PlainText renders the layout as one string.
func (l TextLayout) PlainText() string {
	var b strings.Builder
	for _, line := range l.Lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
	}
	return b.String()
}

func pageGlyphs(content Content) []layoutGlyph {
	glyphs := make([]layoutGlyph, 0, len(content.Text))
	for _, t := range content.Text {
		s := strings.ReplaceAll(t.S, "\u00a0", " ")
		s = strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == '\t' || r == '\uFFFD' {
				return -1
			}
			return r
		}, s)
		if s == "" {
			continue
		}
		size := math.Abs(t.FontSize)
		if size == 0 {
			size = math.Max(1, math.Abs(t.W))
		}
		glyphs = append(glyphs, layoutGlyph{
			text: s,
			x:    t.X,
			y:    t.Y,
			w:    math.Abs(t.W),
			size: size,
		})
	}
	return glyphs
}

func groupGlyphRows(glyphs []layoutGlyph) []layoutRow {
	sort.SliceStable(glyphs, func(i, j int) bool {
		if sameBaseline(glyphs[i], glyphs[j]) {
			return glyphs[i].x < glyphs[j].x
		}
		return glyphs[i].y > glyphs[j].y
	})

	var rows []layoutRow
	for _, glyph := range glyphs {
		rowIndex := -1
		for i := range rows {
			if math.Abs(glyph.y-rows[i].y) <= baselineTolerance(glyph.size, rows[i].fontSize()) {
				rowIndex = i
				break
			}
		}
		if rowIndex < 0 {
			rows = append(rows, layoutRow{y: glyph.y})
			rowIndex = len(rows) - 1
		}
		rows[rowIndex].glyphs = append(rows[rowIndex].glyphs, glyph)
		rows[rowIndex].y = averageGlyphY(rows[rowIndex].glyphs)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].y > rows[j].y })
	return rows
}

func splitRowsIntoLines(rows []layoutRow) []layoutRow {
	out := make([]layoutRow, 0, len(rows))
	for _, row := range rows {
		sort.SliceStable(row.glyphs, func(i, j int) bool { return row.glyphs[i].x < row.glyphs[j].x })
		row.lines = buildRowFragments(row.glyphs, row.y)
		if len(row.lines) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func buildRowFragments(glyphs []layoutGlyph, y float64) []TextLine {
	metrics := rowMetrics(glyphs)
	wordGap := metrics.wordGap()
	columnGap := metrics.columnGap(wordGap)
	var lines []TextLine
	var b strings.Builder
	var x, end, size float64
	have := false

	flush := func() {
		text := strings.TrimSpace(b.String())
		if text != "" {
			lines = append(lines, TextLine{X: x, Y: y, FontSize: size, Text: text})
		}
		b.Reset()
		have = false
	}

	for _, glyph := range glyphs {
		if !have {
			x, size = glyph.x, glyph.size
		} else {
			gap := glyph.x - end
			if gap > columnGap {
				flush()
				x, size = glyph.x, glyph.size
			} else if gap > wordGap && shouldInsertSpace(b.String(), glyph.text) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(glyph.text)
		end = glyphEnd(glyph)
		if glyph.size > size {
			size = glyph.size
		}
		have = true
	}
	flush()
	return lines
}

func orderLayoutLines(rows []layoutRow) []TextLine {
	columnStart := firstColumnRow(rows)
	if columnStart < 0 {
		return flattenRows(rows)
	}

	lines := flattenRows(rows[:columnStart])
	split := columnSplit(rows[columnStart])
	left, right := collectColumns(rows[columnStart:], split)
	lines = append(lines, left...)
	lines = append(lines, right...)
	return lines
}

func flattenRows(rows []layoutRow) []TextLine {
	var lines []TextLine
	for _, row := range rows {
		sort.SliceStable(row.lines, func(i, j int) bool { return row.lines[i].X < row.lines[j].X })
		lines = append(lines, row.lines...)
	}
	return lines
}

func firstColumnRow(rows []layoutRow) int {
	for i, row := range rows {
		if len(row.lines) < 2 {
			continue
		}
		if row.lines[1].X-row.lines[0].X > math.Max(row.fontSize()*6, 36) {
			return i
		}
	}
	return -1
}

func collectColumns(rows []layoutRow, split float64) ([]TextLine, []TextLine) {
	var left, right []TextLine
	for _, row := range rows {
		for _, line := range row.lines {
			if line.X < split {
				left = append(left, line)
			} else {
				right = append(right, line)
			}
		}
	}
	return left, right
}

func columnSplit(row layoutRow) float64 {
	sort.SliceStable(row.lines, func(i, j int) bool { return row.lines[i].X < row.lines[j].X })
	return (row.lines[0].X + row.lines[1].X) / 2
}

type rowMetricSet struct {
	widths []float64
	gaps   []float64
	size   float64
	count  int
}

func rowMetrics(glyphs []layoutGlyph) rowMetricSet {
	var m rowMetricSet
	for i, glyph := range glyphs {
		m.count++
		if glyph.w > 0 {
			m.widths = append(m.widths, glyph.w)
		}
		if glyph.size > m.size {
			m.size = glyph.size
		}
		if i > 0 {
			gap := glyph.x - glyphEnd(glyphs[i-1])
			if gap > 0.05 {
				m.gaps = append(m.gaps, gap)
			}
		}
	}
	sort.Float64s(m.widths)
	sort.Float64s(m.gaps)
	return m
}

func (m rowMetricSet) wordGap() float64 {
	base := math.Max(0.8, m.size*0.10)
	if len(m.widths) > 0 {
		base = math.Max(base, median(m.widths)*0.25)
	}
	if len(m.gaps) > 5 && len(m.gaps)*3 > m.count {
		q1 := m.gaps[len(m.gaps)/4]
		q3 := m.gaps[(len(m.gaps)*3)/4]
		if q1 > base && q3/q1 < 1.5 {
			return math.Min(q1*1.3, math.Max(base, m.size*0.4))
		}
	}
	return base
}

func (m rowMetricSet) columnGap(wordGap float64) float64 {
	return math.Max(math.Max(m.size*5, wordGap*5), 36)
}

func sameBaseline(a, b layoutGlyph) bool {
	return math.Abs(a.y-b.y) <= baselineTolerance(a.size, b.size)
}

func baselineTolerance(a, b float64) float64 {
	size := math.Max(a, b)
	if size == 0 {
		return 2
	}
	return math.Max(1.5, size*0.45)
}

func averageGlyphY(glyphs []layoutGlyph) float64 {
	if len(glyphs) == 0 {
		return 0
	}
	sum := 0.0
	for _, glyph := range glyphs {
		sum += glyph.y
	}
	return sum / float64(len(glyphs))
}

func (r layoutRow) fontSize() float64 {
	size := 0.0
	for _, line := range r.lines {
		if line.FontSize > size {
			size = line.FontSize
		}
	}
	for _, glyph := range r.glyphs {
		if glyph.size > size {
			size = glyph.size
		}
	}
	return size
}

func glyphEnd(g layoutGlyph) float64 {
	if g.w > 0 {
		return g.x + g.w
	}
	return g.x
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)/2]
}

func shouldInsertSpace(current, next string) bool {
	if current == "" || next == "" {
		return false
	}
	last := []rune(current)[len([]rune(current))-1]
	first := []rune(next)[0]
	if unicode.IsSpace(last) || unicode.IsSpace(first) {
		return false
	}
	return !strings.ContainsRune(".,;:!?)]}%", first)
}
