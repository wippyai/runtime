package vt

import "strings"

// RenderLine renders the buffer line at absolute index y as a single ANSI
// string. Trailing cells without content or styling are trimmed and a styled
// line ends with a reset. Indices outside the buffer render as an empty string.
func (b *Buffer) RenderLine(y int) string {
	if y < 0 || y >= b.Lines.Length() {
		return ""
	}
	line := b.Lines.Get(y)
	if line == nil {
		return ""
	}
	var (
		out          strings.Builder
		style        = &stringSerializeHandler{}
		cell         = NewCellData()
		prev         = NewCellData()
		significant  int
		sigStyled    bool
		prevStyled   bool
		pendingSpace int
	)
	for x := 0; x < line.Len; x++ {
		line.LoadCell(x, cell)
		if cell.GetWidth() == 0 {
			continue
		}
		chars, width := cell.GetChars(), cell.GetWidth()
		blank := chars == "" || chars == " "
		if seq := style.diffStyle(cell, prev); len(seq) > 0 {
			out.WriteString(strings.Repeat(" ", pendingSpace))
			pendingSpace = 0
			out.WriteString("\x1b[" + strings.Join(seq, ";") + "m")
			prevStyled = !cell.IsAttributeDefault()
			prev, cell = cell, prev
		}
		if blank {
			pendingSpace += width
			if prevStyled {
				out.WriteString(strings.Repeat(" ", pendingSpace))
				pendingSpace = 0
				significant, sigStyled = out.Len(), prevStyled
			}
			continue
		}
		out.WriteString(strings.Repeat(" ", pendingSpace))
		pendingSpace = 0
		out.WriteString(chars)
		significant, sigStyled = out.Len(), prevStyled
	}
	rendered := out.String()[:significant]
	if sigStyled {
		rendered += "\x1b[0m"
	}
	return rendered
}
