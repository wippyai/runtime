// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"strconv"
	"strings"

	xterm "github.com/gitpod-io/xterm-go"
)

// renderLine renders the buffer line at absolute index y as one ANSI string.
// Trailing cells without content or styling are trimmed and a styled line ends
// with a reset. Indices outside the buffer render as an empty string.
func renderLine(buffer *xterm.Buffer, y int) string {
	if y < 0 || y >= buffer.Lines.Length() {
		return ""
	}
	line := buffer.Lines.Get(y)
	if line == nil {
		return ""
	}
	var (
		out         strings.Builder
		cell        = xterm.NewCellData()
		current     = xterm.NewCellData()
		styled      bool
		pending     int
		significant int
		sigStyled   bool
	)
	for x := range line.Len {
		line.LoadCell(x, cell)
		width := cell.GetWidth()
		if width == 0 {
			continue
		}
		chars := cell.GetChars()
		blank := chars == "" || chars == " "
		if !cell.AttributesEqual(current) {
			out.WriteString(strings.Repeat(" ", pending))
			pending = 0
			out.WriteString(sgr(cell))
			styled = !cell.IsAttributeDefault()
			cell, current = current, cell
		}
		if blank {
			pending += width
			if styled {
				out.WriteString(strings.Repeat(" ", pending))
				pending = 0
				significant, sigStyled = out.Len(), styled
			}
			continue
		}
		out.WriteString(strings.Repeat(" ", pending))
		pending = 0
		out.WriteString(chars)
		significant, sigStyled = out.Len(), styled
	}
	rendered := out.String()[:significant]
	if sigStyled {
		rendered += "\x1b[0m"
	}
	return rendered
}

// sgr returns the escape sequence that sets exactly the attributes of cell.
func sgr(cell *xterm.CellData) string {
	params := []string{"0"}
	flags := []struct {
		set  uint32
		code string
	}{
		{cell.IsBold(), "1"}, {cell.IsDim(), "2"}, {cell.IsItalic(), "3"},
		{cell.IsUnderline(), "4"}, {cell.IsBlink(), "5"}, {cell.IsInverse(), "7"},
		{cell.IsInvisible(), "8"}, {cell.IsStrikethrough(), "9"}, {cell.IsOverline(), "53"},
	}
	for _, flag := range flags {
		if flag.set != 0 {
			params = append(params, flag.code)
		}
	}
	params = appendColor(params, cell.GetFgColor(), cell.IsFgRGB(), cell.IsFgPalette(), 30, 90, "38")
	params = appendColor(params, cell.GetBgColor(), cell.IsBgRGB(), cell.IsBgPalette(), 40, 100, "48")
	return "\x1b[" + strings.Join(params, ";") + "m"
}

func appendColor(params []string, color int, rgb, palette bool, base, bright int, extended string) []string {
	switch {
	case rgb:
		return append(params, extended, "2", strconv.Itoa(color>>16&0xff), strconv.Itoa(color>>8&0xff), strconv.Itoa(color&0xff))
	case palette && color >= 16:
		return append(params, extended, "5", strconv.Itoa(color))
	case palette && color >= 8:
		return append(params, strconv.Itoa(bright+color-8))
	case palette:
		return append(params, strconv.Itoa(base+color))
	}
	return params
}
