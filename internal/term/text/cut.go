// SPDX-License-Identifier: MPL-2.0

package text

import "strings"

// Truncate limits s to length cells. When s is wider, the cut text is
// followed by tail, which counts toward length. Grapheme clusters are never
// split. Escape sequences are kept in full, including those after the cut, so
// styling and hyperlink state stay balanced.
func Truncate(s string, length int, tail string) string {
	if Width(s) <= length {
		return s
	}
	length -= Width(tail)
	if length < 0 {
		return ""
	}
	var b strings.Builder
	cur := 0
	cut := false
	for len(s) > 0 {
		t := Next(s)
		s = s[len(t.Text):]
		switch t.Kind {
		case Cluster:
			if cut {
				continue
			}
			if cur+t.Width > length {
				cut = true
				b.WriteString(tail)
				continue
			}
			cur += t.Width
			b.WriteString(t.Text)
		case Control:
			if !cut {
				b.WriteString(t.Text)
			}
		default:
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// Cut returns the cells in [left, right) of s. A grapheme cluster is kept
// only when it lies wholly inside the range. Escape sequences before, inside
// and after the range are kept in full so styling state is preserved.
func Cut(s string, left, right int) string {
	if right <= left {
		return ""
	}
	left = max(left, 0)
	var b strings.Builder
	cur := 0
	for len(s) > 0 {
		t := Next(s)
		s = s[len(t.Text):]
		switch t.Kind {
		case Cluster:
			if cur >= left && cur+t.Width <= right {
				b.WriteString(t.Text)
			}
			cur += t.Width
		case Control:
			if cur >= left && cur < right {
				b.WriteString(t.Text)
			}
		default:
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
