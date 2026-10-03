// SPDX-License-Identifier: MPL-2.0

package canvas

import "github.com/wippyai/runtime/internal/term/text"

// Decode walks one styled display row and calls emit with the starting column
// and cell of every printable grapheme cluster that has width. SGR and OSC 8
// sequences update the style and link carried by following cells. Other escape
// sequences and control characters produce no cells, and a line feed ends the
// row. Decoding stops when emit returns false.
func Decode(row string, emit func(col int, cell Cell) bool) {
	var (
		style text.Style
		link  Link
		col   int
	)
	for len(row) > 0 {
		t := text.Next(row)
		row = row[len(t.Text):]
		switch t.Kind {
		case text.Cluster:
			if t.Width == 0 {
				continue
			}
			if !emit(col, Cell{Content: t.Text, Width: t.Width, Style: style, Link: link}) {
				return
			}
			col += t.Width
		case text.Control:
			if t.Text == "\n" {
				return
			}
		case text.CSI:
			if body, ok := t.SGR(); ok {
				style.ApplySGR(body)
			}
		case text.OSC:
			if params, url, ok := t.Hyperlink(); ok {
				link = Link{Params: params, URL: url}
				if url == "" {
					link = Link{}
				}
			}
		}
	}
}

// Parse decodes row into a line of width cells. Cells beyond width are
// dropped; unfilled columns are blank.
func Parse(row string, width int) Line {
	b := NewBuffer(width, 1)
	b.DrawRow(0, 0, width, 0, row)
	return b.Line(0)
}
