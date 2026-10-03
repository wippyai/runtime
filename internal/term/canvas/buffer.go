// SPDX-License-Identifier: MPL-2.0

package canvas

import (
	"strings"

	"github.com/wippyai/runtime/internal/term/text"
)

// Line is one row of cells.
type Line []Cell

// Set stores c at column x. Overwriting part of a wide cell blanks the rest of
// it, and a cell that does not fit in the line becomes styled blanks. A nil c
// stores a blank.
func (l Line) Set(x int, c *Cell) {
	if x < 0 || x >= len(l) {
		return
	}
	if prev := l[x]; prev.Width > 1 {
		for j := 0; j < prev.Width && x+j < len(l); j++ {
			l[x+j] = prev.Blank()
		}
	} else if prev.Width == 0 {
		for j := 1; x-j >= 0; j++ {
			if wide := l[x-j]; wide.Width > 1 && j < wide.Width {
				for k := range wide.Width {
					l[x-j+k] = wide.Blank()
				}
				break
			}
		}
	}
	if c == nil {
		l[x] = EmptyCell
		return
	}
	l[x] = *c
	if x+c.Width > len(l) {
		for i := 0; i < c.Width && x+i < len(l); i++ {
			l[x+i] = c.Blank()
		}
		return
	}
	for j := 1; j < c.Width; j++ {
		l[x+j] = Cell{}
	}
}

// Render returns the line as a string with the minimal SGR and hyperlink
// sequences. Trailing unstyled blanks are omitted.
func (l Line) Render() string {
	var b strings.Builder
	renderLine(&b, l)
	return b.String()
}

func renderLine(b *strings.Builder, l Line) {
	var (
		pen     text.Style
		link    Link
		pending int
	)
	for _, c := range l {
		if c.IsZero() {
			continue
		}
		if c == EmptyCell {
			if !pen.IsZero() {
				b.WriteString(text.ResetStyle)
				pen = text.Style{}
			}
			if !link.IsZero() {
				b.WriteString(text.ResetHyperlink())
				link = Link{}
			}
			pending++
			continue
		}
		for ; pending > 0; pending-- {
			b.WriteByte(' ')
		}
		if c.Style.IsZero() && !pen.IsZero() {
			b.WriteString(text.ResetStyle)
			pen = text.Style{}
		}
		if c.Style != pen {
			b.WriteString(c.Style.Diff(pen))
			pen = c.Style
		}
		if c.Link != link && !link.IsZero() {
			b.WriteString(text.ResetHyperlink())
			link = Link{}
		}
		if c.Link != link {
			b.WriteString(text.SetHyperlink(c.Link.URL, c.Link.Params))
			link = c.Link
		}
		b.WriteString(c.Content)
	}
	if !link.IsZero() {
		b.WriteString(text.ResetHyperlink())
	}
	if !pen.IsZero() {
		b.WriteString(text.ResetStyle)
	}
}

// Buffer is a fixed-size grid of cells.
type Buffer struct {
	lines []Line
}

// NewBuffer returns a width by height buffer of blank cells.
func NewBuffer(width, height int) *Buffer {
	b := &Buffer{lines: make([]Line, height)}
	for y := range b.lines {
		b.lines[y] = make(Line, width)
		for x := range b.lines[y] {
			b.lines[y][x] = EmptyCell
		}
	}
	return b
}

// Width returns the column count.
func (b *Buffer) Width() int {
	if len(b.lines) == 0 {
		return 0
	}
	return len(b.lines[0])
}

// Height returns the row count.
func (b *Buffer) Height() int { return len(b.lines) }

// Line returns row y, or nil when out of range.
func (b *Buffer) Line(y int) Line {
	if y < 0 || y >= len(b.lines) {
		return nil
	}
	return b.lines[y]
}

// At returns the cell at x, y, or nil when out of range.
func (b *Buffer) At(x, y int) *Cell {
	line := b.Line(y)
	if x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}

// Set stores c at x, y. See Line.Set.
func (b *Buffer) Set(x, y int, c *Cell) {
	b.Line(y).Set(x, c)
}

// Clear blanks every cell.
func (b *Buffer) Clear() {
	for _, line := range b.lines {
		for x := range line {
			line[x] = EmptyCell
		}
	}
}

// FillRows copies row into every row of b. row is truncated or left short of
// the buffer width as it is.
func (b *Buffer) FillRows(row Line) {
	for _, line := range b.lines {
		copy(line, row)
	}
}

// DrawRow clears the width cells at x, y and draws the styled row s into
// them, skipping the first skip columns of s. A cell that straddles either
// edge of the window is not drawn. The window is clipped to the buffer.
func (b *Buffer) DrawRow(x, y, width, skip int, s string) {
	line := b.Line(y)
	if x < 0 || x >= len(line) {
		return
	}
	width = min(width, len(line)-x)
	for i := range width {
		line.Set(x+i, nil)
	}
	Decode(s, func(col int, c Cell) bool {
		if col < skip {
			return true
		}
		rel := col - skip
		if rel+c.Width > width {
			return false
		}
		line.Set(x+rel, &c)
		return true
	})
}

// Render returns the buffer as rows joined by line feeds. See Line.Render.
func (b *Buffer) Render() string {
	var out strings.Builder
	for y, line := range b.lines {
		renderLine(&out, line)
		if y < len(b.lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
