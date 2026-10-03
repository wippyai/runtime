// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strings"
)

const tabWidth = 4

// Box describes how a block of text is styled and laid out: colors and
// attributes, padding, border, margin, size limits and alignment. Setters
// return a modified copy, so a Box is a value and is safe to share.
type Box struct {
	fg, bg Color
	attrs  Attr
	under  bool

	width, height       int
	maxWidth, maxHeight int
	alignH, alignV      Position
	inline              bool

	padding, margin [4]int

	border    Border
	hasBorder bool
	sides     [4]bool
	borderFg  [4]Color
	borderBg  [4]Color
}

// Foreground sets the text color.
func (b Box) Foreground(c Color) Box { b.fg = c; return b }

// Background sets the background color.
func (b Box) Background(c Color) Box { b.bg = c; return b }

func (b Box) attr(a Attr, on bool) Box {
	if on {
		b.attrs |= a
	} else {
		b.attrs &^= a
	}
	return b
}

// Bold toggles bold text.
func (b Box) Bold(on bool) Box { return b.attr(AttrBold, on) }

// Italic toggles italic text.
func (b Box) Italic(on bool) Box { return b.attr(AttrItalic, on) }

// Underline toggles underlined text.
func (b Box) Underline(on bool) Box { b.under = on; return b }

// Strikethrough toggles struck-through text.
func (b Box) Strikethrough(on bool) Box { return b.attr(AttrStrikethrough, on) }

// Faint toggles dim text.
func (b Box) Faint(on bool) Box { return b.attr(AttrFaint, on) }

// Blink toggles blinking text.
func (b Box) Blink(on bool) Box { return b.attr(AttrBlink, on) }

// Reverse toggles reverse video.
func (b Box) Reverse(on bool) Box { return b.attr(AttrReverse, on) }

// Width sets the block width including padding, excluding border and margin.
// Longer lines wrap at spaces.
func (b Box) Width(n int) Box { b.width = n; return b }

// Height sets the minimum block height including padding.
func (b Box) Height(n int) Box { b.height = n; return b }

// MaxWidth truncates every output line to n cells.
func (b Box) MaxWidth(n int) Box { b.maxWidth = n; return b }

// MaxHeight keeps at most n output lines.
func (b Box) MaxHeight(n int) Box { b.maxHeight = n; return b }

// Align sets horizontal alignment of lines within the block.
func (b Box) Align(p Position) Box { b.alignH = p; return b }

// AlignVertical sets vertical alignment of content within the block height.
func (b Box) AlignVertical(p Position) Box { b.alignV = p; return b }

// Inline renders on one line, ignoring newlines, padding, border and margin.
func (b Box) Inline(on bool) Box { b.inline = on; return b }

// sidesOf expands CSS-style 1 to 4 values to top, right, bottom, left.
func sidesOf[T any](v []T) (out [4]T, ok bool) {
	switch len(v) {
	case 1:
		return [4]T{v[0], v[0], v[0], v[0]}, true
	case 2:
		return [4]T{v[0], v[1], v[0], v[1]}, true
	case 3:
		return [4]T{v[0], v[1], v[2], v[1]}, true
	case 4:
		return [4]T{v[0], v[1], v[2], v[3]}, true
	}
	return out, false
}

// Padding sets padding with CSS shorthand: 1 value for all sides, 2 for
// vertical and horizontal, 3 for top, horizontal and bottom, 4 for each side
// clockwise from the top. Other counts leave the padding unchanged.
func (b Box) Padding(v ...int) Box {
	if p, ok := sidesOf(v); ok {
		b.padding = p
	}
	return b
}

// Margin sets margin with the same shorthand as Padding.
func (b Box) Margin(v ...int) Box {
	if m, ok := sidesOf(v); ok {
		b.margin = m
	}
	return b
}

// Border draws border around the block. Sides follow the Padding shorthand
// with booleans; with no sides every side is drawn.
func (b Box) Border(border Border, sides ...bool) Box {
	b.border, b.hasBorder = border, true
	s, ok := sidesOf(sides)
	if !ok {
		s = [4]bool{true, true, true, true}
	}
	b.sides = s
	return b
}

// BorderForeground colors border sides with the Padding shorthand.
func (b Box) BorderForeground(c ...Color) Box {
	if s, ok := sidesOf(c); ok {
		b.borderFg = s
	}
	return b
}

// BorderBackground sets border side backgrounds with the Padding shorthand.
func (b Box) BorderBackground(c ...Color) Box {
	if s, ok := sidesOf(c); ok {
		b.borderBg = s
	}
	return b
}

// Render joins strs with spaces and applies the box.
func (b Box) Render(strs ...string) string {
	s := strings.Join(strs, " ")
	s = strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
	if b == (Box{}) {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if b.inline {
		s = strings.ReplaceAll(s, "\n", "")
	}
	pad := b.padding
	if !b.inline && b.width > 0 {
		s = Wrap(s, b.width-pad[sideLeft]-pad[sideRight])
	}

	text := Style{Fg: b.fg, Bg: b.bg, Attrs: b.attrs}
	if b.under {
		text.Underline = UnderlineSingle
	}
	space := Style{Bg: b.bg}
	if b.attrs&AttrReverse != 0 {
		space = Style{Fg: b.fg, Bg: b.bg, Attrs: AttrReverse}
	}

	rows := strings.Split(s, "\n")
	for i, row := range rows {
		if row != "" {
			rows[i] = text.Styled(row)
		}
	}
	s = strings.Join(rows, "\n")

	if !b.inline {
		s = padColumns(s, pad[sideLeft], pad[sideRight], space)
		s = strings.Repeat("\n", max(0, pad[sideTop])) + s + strings.Repeat("\n", max(0, pad[sideBottom]))
	}
	if b.height > 0 {
		s = alignVertical(s, b.alignV, b.height)
	}
	if strings.Contains(s, "\n") || b.width != 0 {
		s = alignHorizontal(s, b.alignH, b.width, space)
	}
	if !b.inline {
		s = b.applyBorder(s)
		s = b.applyMargin(s)
	}
	if b.maxWidth > 0 {
		rows = strings.Split(s, "\n")
		for i, row := range rows {
			rows[i] = Truncate(row, b.maxWidth, "")
		}
		s = strings.Join(rows, "\n")
	}
	if b.maxHeight > 0 {
		rows = strings.Split(s, "\n")
		s = strings.Join(rows[:min(b.maxHeight, len(rows))], "\n")
	}
	return s
}

func padColumns(s string, left, right int, style Style) string {
	if left <= 0 && right <= 0 {
		return s
	}
	l, r := style.Styled(spaces(left)), style.Styled(spaces(right))
	rows := strings.Split(s, "\n")
	for i, row := range rows {
		rows[i] = l + row + r
	}
	return strings.Join(rows, "\n")
}

func (b Box) applyMargin(s string) string {
	m := b.margin
	if m == [4]int{} {
		return s
	}
	s = padColumns(s, m[sideLeft], m[sideRight], Style{})
	_, width := lines(s)
	blank := spaces(width)
	if m[sideTop] > 0 {
		s = strings.Repeat(blank+"\n", m[sideTop]) + s
	}
	if m[sideBottom] > 0 {
		s += strings.Repeat("\n"+blank, m[sideBottom])
	}
	return s
}

func alignHorizontal(s string, pos Position, width int, space Style) string {
	rows, widest := lines(s)
	var b strings.Builder
	for i, row := range rows {
		short := widest - Width(row)
		short += max(0, width-(short+Width(row)))
		if short > 0 {
			switch pos {
			case Right:
				row = space.Styled(spaces(short)) + row
			case Center:
				left := short / 2
				row = space.Styled(spaces(left)) + row + space.Styled(spaces(left+short%2))
			default:
				row += space.Styled(spaces(short))
			}
		}
		b.WriteString(row)
		if i < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func alignVertical(s string, pos Position, height int) string {
	gap := height - Height(s)
	if gap < 0 {
		return s
	}
	switch pos {
	case Top:
		return s + strings.Repeat("\n", gap)
	case Center:
		top, bottom := gap/2, gap/2
		if top+bottom < gap {
			bottom++
		}
		return strings.Repeat("\n", top) + s + strings.Repeat("\n", bottom)
	case Bottom:
		return strings.Repeat("\n", gap) + s
	}
	return s
}
