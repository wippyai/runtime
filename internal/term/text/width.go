// SPDX-License-Identifier: MPL-2.0

package text

import "strings"

// Width returns the number of terminal cells s occupies on one line. Escape
// sequences and control characters are zero width.
func Width(s string) int {
	width := 0
	for len(s) > 0 {
		t := Next(s)
		width += t.Width
		s = s[len(t.Text):]
	}
	return width
}

// ClusterWidth returns the cell width of the first grapheme cluster of s.
func ClusterWidth(s string) int {
	if s == "" {
		return 0
	}
	return Next(s).Width
}

// Strip removes escape sequences and returns the printable text. Control
// characters other than ESC are kept.
func Strip(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		t := Next(s)
		if t.Kind == Cluster || t.Kind == Control {
			b.WriteString(t.Text)
		}
		s = s[len(t.Text):]
	}
	return b.String()
}

// Height returns the number of lines in s.
func Height(s string) int { return strings.Count(s, "\n") + 1 }

// MaxWidth returns the width of the widest line of s.
func MaxWidth(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		widest = max(widest, Width(line))
	}
	return widest
}

// Size returns MaxWidth and Height of s.
func Size(s string) (width, height int) { return MaxWidth(s), Height(s) }
