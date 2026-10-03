// SPDX-License-Identifier: MPL-2.0

package text

import "strings"

// Border is the set of glyphs drawn around a block.
type Border struct {
	Top, Bottom, Left, Right                   string
	TopLeft, TopRight, BottomLeft, BottomRight string
}

// Standard borders.
var (
	NormalBorder  = Border{"─", "─", "│", "│", "┌", "┐", "└", "┘"}
	RoundedBorder = Border{"─", "─", "│", "│", "╭", "╮", "╰", "╯"}
	ThickBorder   = Border{"━", "━", "┃", "┃", "┏", "┓", "┗", "┛"}
	DoubleBorder  = Border{"═", "═", "║", "║", "╔", "╗", "╚", "╝"}
	HiddenBorder  = Border{" ", " ", " ", " ", " ", " ", " ", " "}
)

// sides indexes per-side values in top, right, bottom, left order.
const (
	sideTop = iota
	sideRight
	sideBottom
	sideLeft
)

func (b Box) applyBorder(s string) string {
	if !b.hasBorder {
		return s
	}
	hasTop, hasRight, hasBottom, hasLeft := b.sides[sideTop], b.sides[sideRight], b.sides[sideBottom], b.sides[sideLeft]
	if !hasTop && !hasRight && !hasBottom && !hasLeft {
		return s
	}
	border := b.border
	rows, width := lines(s)

	glyph := func(g string) string {
		if g == "" {
			return " "
		}
		return g
	}
	cluster := func(g string) string {
		if g == "" {
			return g
		}
		return Next(g).Text
	}
	border.Left, border.Right = glyph(border.Left), glyph(border.Right)
	if hasLeft {
		width += Width(border.Left)
	}
	border.TopLeft, border.TopRight = glyph(border.TopLeft), glyph(border.TopRight)
	border.BottomLeft, border.BottomRight = glyph(border.BottomLeft), glyph(border.BottomRight)
	if !hasLeft {
		border.TopLeft, border.BottomLeft = "", ""
	}
	if !hasRight {
		border.TopRight, border.BottomRight = "", ""
	}
	border.TopLeft, border.TopRight = cluster(border.TopLeft), cluster(border.TopRight)
	border.BottomLeft, border.BottomRight = cluster(border.BottomLeft), cluster(border.BottomRight)

	var out strings.Builder
	if hasTop {
		out.WriteString(b.styleBorder(horizontalEdge(border.TopLeft, border.Top, border.TopRight, width), sideTop))
		out.WriteByte('\n')
	}
	left, right := []rune(border.Left), []rune(border.Right)
	for i, line := range rows {
		if hasLeft {
			out.WriteString(b.styleBorder(string(left[i%len(left)]), sideLeft))
		}
		out.WriteString(line)
		if hasRight {
			out.WriteString(b.styleBorder(string(right[i%len(right)]), sideRight))
		}
		if i < len(rows)-1 {
			out.WriteByte('\n')
		}
	}
	if hasBottom {
		out.WriteByte('\n')
		out.WriteString(b.styleBorder(horizontalEdge(border.BottomLeft, border.Bottom, border.BottomRight, width), sideBottom))
	}
	return out.String()
}

// horizontalEdge draws a top or bottom edge totalling width plus the right
// corner, repeating the middle glyph runes.
func horizontalEdge(left, middle, right string, width int) string {
	if middle == "" {
		middle = " "
	}
	runes := []rune(middle)
	rightWidth := Width(right)
	var out strings.Builder
	out.WriteString(left)
	for i, j := Width(left)+rightWidth, 0; i < width+rightWidth; j++ {
		r := string(runes[j%len(runes)])
		out.WriteString(r)
		i += max(1, Width(r))
	}
	out.WriteString(right)
	return out.String()
}

func (b Box) styleBorder(glyph string, side int) string {
	return Style{Fg: b.borderFg[side], Bg: b.borderBg[side]}.Styled(glyph)
}
