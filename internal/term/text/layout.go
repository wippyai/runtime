// SPDX-License-Identifier: MPL-2.0

package text

import (
	"math"
	"strings"
)

// Position is a fractional alignment from 0 (top or left) to 1 (bottom or
// right). Values outside the range are clamped where fractions are applied.
type Position float64

// Alignment anchors.
const (
	Top    Position = 0
	Bottom Position = 1
	Center Position = 0.5
	Left   Position = 0
	Right  Position = 1
)

// share returns how many of n cells lie before the content: none at 0, all at
// 1, rounded in between.
func (p Position) share(n int) int {
	return int(math.Round(float64(n) * math.Min(1, math.Max(0, float64(p)))))
}

// lines splits s on newlines and returns the width of the widest line.
func lines(s string) ([]string, int) {
	l := strings.Split(s, "\n")
	widest := 0
	for _, line := range l {
		widest = max(widest, Width(line))
	}
	return l, widest
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// JoinHorizontal places blocks side by side. Shorter blocks are aligned
// vertically by pos and every block is padded to its own widest line.
func JoinHorizontal(pos Position, blocks ...string) string {
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	var (
		rows      = make([][]string, len(blocks))
		maxWidths = make([]int, len(blocks))
		maxHeight int
	)
	for i, block := range blocks {
		rows[i], maxWidths[i] = lines(block)
		maxHeight = max(maxHeight, len(rows[i]))
	}
	for i := range rows {
		extra := maxHeight - len(rows[i])
		if extra == 0 {
			continue
		}
		top := pos.share(extra)
		bottom := extra - top
		padded := make([]string, 0, maxHeight)
		padded = append(padded, make([]string, top)...)
		padded = append(padded, rows[i]...)
		padded = append(padded, make([]string, bottom)...)
		rows[i] = padded
	}
	var b strings.Builder
	for y := 0; y < maxHeight; y++ {
		for x, block := range rows {
			b.WriteString(block[y])
			b.WriteString(spaces(maxWidths[x] - Width(block[y])))
		}
		if y < maxHeight-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// JoinVertical stacks blocks. Lines are aligned horizontally by pos within
// the widest line.
func JoinVertical(pos Position, blocks ...string) string {
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	rows := make([][]string, len(blocks))
	maxWidth := 0
	for i, block := range blocks {
		var w int
		rows[i], w = lines(block)
		maxWidth = max(maxWidth, w)
	}
	var b strings.Builder
	for i, block := range rows {
		for j, line := range block {
			gap := maxWidth - Width(line)
			left := pos.share(gap)
			right := gap - left
			b.WriteString(spaces(left))
			b.WriteString(line)
			b.WriteString(spaces(right))
			if i != len(rows)-1 || j != len(block)-1 {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// PlaceHorizontal pads s to width cells, positioning the content by pos.
func PlaceHorizontal(width int, pos Position, s string) string {
	rows, contentWidth := lines(s)
	gap := width - contentWidth
	if gap <= 0 {
		return s
	}
	var b strings.Builder
	for i, line := range rows {
		total := gap + max(0, contentWidth-Width(line))
		left := pos.share(total)
		right := total - left
		b.WriteString(spaces(left))
		b.WriteString(line)
		b.WriteString(spaces(right))
		if i < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// PlaceVertical pads s to height lines, positioning the content by pos.
func PlaceVertical(height int, pos Position, s string) string {
	gap := height - Height(s)
	if gap <= 0 {
		return s
	}
	_, width := lines(s)
	empty := spaces(width)
	top := pos.share(gap)
	bottom := gap - top
	var b strings.Builder
	for i := 0; i < top; i++ {
		b.WriteString(empty)
		b.WriteByte('\n')
	}
	b.WriteString(s)
	for i := 0; i < bottom; i++ {
		b.WriteByte('\n')
		b.WriteString(empty)
	}
	return b.String()
}

// Place positions s inside a width by height box.
func Place(width, height int, h, v Position, s string) string {
	return PlaceVertical(height, v, PlaceHorizontal(width, h, s))
}
