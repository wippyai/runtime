// SPDX-License-Identifier: MPL-2.0

package text

import "strings"

// Attr is a set of SGR text attributes.
type Attr uint8

// Text attributes.
const (
	AttrBold Attr = 1 << iota
	AttrFaint
	AttrItalic
	AttrBlink
	AttrRapidBlink
	AttrReverse
	AttrConceal
	AttrStrikethrough
)

// Underline is an SGR underline style.
type Underline uint8

// Underline styles.
const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// ResetStyle is the SGR sequence clearing all attributes and colors.
const ResetStyle = "\x1b[m"

// Style is the SGR state of a cell. The zero value is the terminal default.
type Style struct {
	Fg             Color
	Bg             Color
	UnderlineColor Color
	Underline      Underline
	Attrs          Attr
}

// IsZero reports whether s is the terminal default style.
func (s Style) IsZero() bool { return s == Style{} }

// Styled wraps str in s and a reset. A zero style returns str unchanged.
func (s Style) Styled(str string) string {
	if s.IsZero() {
		return str
	}
	return s.String() + str + ResetStyle
}

// String returns the SGR sequence that selects s from the default state.
func (s Style) String() string {
	if s.IsZero() {
		return ResetStyle
	}
	var params []string
	params = s.appendAttrs(params, s.Attrs)
	params = s.appendUnderline(params)
	if !s.Fg.IsNone() {
		params = s.Fg.appendSGR(params, 30)
	}
	if !s.Bg.IsNone() {
		params = s.Bg.appendSGR(params, 40)
	}
	if !s.UnderlineColor.IsNone() {
		params = s.UnderlineColor.appendUnderlineSGR(params)
	}
	return sgr(params)
}

func sgr(params []string) string {
	return "\x1b[" + strings.Join(params, ";") + "m"
}

func (Style) appendAttrs(dst []string, attrs Attr) []string {
	for _, a := range [...]struct {
		bit  Attr
		code string
	}{
		{AttrBold, "1"}, {AttrFaint, "2"}, {AttrItalic, "3"}, {AttrBlink, "5"},
		{AttrRapidBlink, "6"}, {AttrReverse, "7"}, {AttrConceal, "8"}, {AttrStrikethrough, "9"},
	} {
		if attrs&a.bit != 0 {
			dst = append(dst, a.code)
		}
	}
	return dst
}

func (s Style) appendUnderline(dst []string) []string {
	switch s.Underline {
	case UnderlineSingle:
		return append(dst, "4")
	case UnderlineDouble:
		return append(dst, "4:2")
	case UnderlineCurly:
		return append(dst, "4:3")
	case UnderlineDotted:
		return append(dst, "4:4")
	case UnderlineDashed:
		return append(dst, "4:5")
	}
	return dst
}

// Diff returns the SGR sequence that moves a terminal from the from style to
// s, or "" when they are equal.
func (s Style) Diff(from Style) string {
	if s == from {
		return ""
	}
	if s.IsZero() {
		return ResetStyle
	}
	var p []string
	if from.Fg != s.Fg {
		p = s.Fg.appendSGR(p, 30)
	}
	if from.Bg != s.Bg {
		p = s.Bg.appendSGR(p, 40)
	}
	if from.UnderlineColor != s.UnderlineColor {
		p = s.UnderlineColor.appendUnderlineSGR(p)
	}

	has := func(st Style, a Attr) bool { return st.Attrs&a != 0 }
	changed := func(a Attr) bool { return has(from, a) != has(s, a) }

	boldChanged, faintChanged := changed(AttrBold), changed(AttrFaint)
	if (boldChanged || faintChanged) &&
		(has(from, AttrBold) && !has(s, AttrBold) || has(from, AttrFaint) && !has(s, AttrFaint)) {
		p = append(p, "22")
		boldChanged, faintChanged = true, true
	}
	if changed(AttrItalic) && !has(s, AttrItalic) {
		p = append(p, "23")
	}
	underlineChanged := from.Underline != s.Underline
	if underlineChanged && s.Underline == UnderlineNone {
		p = append(p, "24")
	}
	blinkChanged, rapidChanged := changed(AttrBlink), changed(AttrRapidBlink)
	if (blinkChanged || rapidChanged) &&
		(has(from, AttrBlink) && !has(s, AttrBlink) || has(from, AttrRapidBlink) && !has(s, AttrRapidBlink)) {
		p = append(p, "25")
		blinkChanged, rapidChanged = true, true
	}
	if changed(AttrReverse) && !has(s, AttrReverse) {
		p = append(p, "27")
	}
	if changed(AttrConceal) && !has(s, AttrConceal) {
		p = append(p, "28")
	}
	if changed(AttrStrikethrough) && !has(s, AttrStrikethrough) {
		p = append(p, "29")
	}

	if boldChanged && has(s, AttrBold) {
		p = append(p, "1")
	}
	if faintChanged && has(s, AttrFaint) {
		p = append(p, "2")
	}
	if changed(AttrItalic) && has(s, AttrItalic) {
		p = append(p, "3")
	}
	if underlineChanged && s.Underline == UnderlineSingle {
		p = append(p, "4")
	}
	if blinkChanged && has(s, AttrBlink) {
		p = append(p, "5")
	}
	if rapidChanged && has(s, AttrRapidBlink) {
		p = append(p, "6")
	}
	if changed(AttrReverse) && has(s, AttrReverse) {
		p = append(p, "7")
	}
	if changed(AttrConceal) && has(s, AttrConceal) {
		p = append(p, "8")
	}
	if changed(AttrStrikethrough) && has(s, AttrStrikethrough) {
		p = append(p, "9")
	}
	if underlineChanged && s.Underline > UnderlineSingle {
		p = s.appendUnderline(p)
	}
	return sgr(p)
}

// sgrParam is one SGR parameter; more marks a ':' sub-parameter follower.
type sgrParam struct {
	value int
	more  bool
}

// ApplySGR updates s with the SGR parameter list body, the bytes between
// "ESC [" and the final "m". Unknown parameters are ignored.
func (s *Style) ApplySGR(body string) {
	var buf [24]sgrParam
	params := buf[:0]
	value := 0
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c >= '0' && c <= '9':
			if value < 1<<20 {
				value = value*10 + int(c-'0')
			}
		case c == ';' || c == ':':
			params = append(params, sgrParam{value: value, more: c == ':'})
			value = 0
		}
	}
	params = append(params, sgrParam{value: value})

	for i := 0; i < len(params); i++ {
		p := params[i]
		switch v := p.value; {
		case v == 0:
			*s = Style{}
		case v == 1:
			s.Attrs |= AttrBold
		case v == 2:
			s.Attrs |= AttrFaint
		case v == 3:
			s.Attrs |= AttrItalic
		case v == 4:
			if p.more && i+1 < len(params) && params[i+1].value <= 5 {
				i++
				s.Underline = Underline(params[i].value)
			} else {
				s.Underline = UnderlineSingle
			}
		case v == 5:
			s.Attrs |= AttrBlink
		case v == 6:
			s.Attrs |= AttrRapidBlink
		case v == 7:
			s.Attrs |= AttrReverse
		case v == 8:
			s.Attrs |= AttrConceal
		case v == 9:
			s.Attrs |= AttrStrikethrough
		case v == 22:
			s.Attrs &^= AttrBold | AttrFaint
		case v == 23:
			s.Attrs &^= AttrItalic
		case v == 24:
			s.Underline = UnderlineNone
		case v == 25:
			s.Attrs &^= AttrBlink | AttrRapidBlink
		case v == 27:
			s.Attrs &^= AttrReverse
		case v == 28:
			s.Attrs &^= AttrConceal
		case v == 29:
			s.Attrs &^= AttrStrikethrough
		case v >= 30 && v <= 37:
			s.Fg = Basic(uint8(v - 30))
		case v == 38, v == 48, v == 58:
			c, n := readExtendedColor(params[i:])
			if n > 0 {
				switch v {
				case 38:
					s.Fg = c
				case 48:
					s.Bg = c
				default:
					s.UnderlineColor = c
				}
				i += n - 1
			}
		case v == 39:
			s.Fg = Color{}
		case v >= 40 && v <= 47:
			s.Bg = Basic(uint8(v - 40))
		case v == 49:
			s.Bg = Color{}
		case v == 59:
			s.UnderlineColor = Color{}
		case v >= 90 && v <= 97:
			s.Fg = Basic(uint8(v - 90 + 8))
		case v >= 100 && v <= 107:
			s.Bg = Basic(uint8(v - 100 + 8))
		}
	}
}

// readExtendedColor decodes the color following a 38, 48 or 58 parameter in
// either the ';' or ':' forms and returns the number of parameters consumed,
// including the leading selector. It returns 0 for malformed colors.
func readExtendedColor(params []sgrParam) (Color, int) {
	if len(params) < 2 {
		return Color{}, 0
	}
	switch params[1].value {
	case 5:
		if len(params) < 3 || params[2].value > 255 {
			return Color{}, 0
		}
		return Indexed(uint8(params[2].value)), 3
	case 2:
		if params[0].more || params[1].more {
			rest := params[2:]
			run := 0
			for run < len(rest) {
				run++
				if !rest[run-1].more {
					break
				}
			}
			var rgb []sgrParam
			switch {
			case run >= 4:
				rgb = rest[run-3 : run]
			case run == 3:
				rgb = rest[:3]
			default:
				return Color{}, 0
			}
			return rgbColor(rgb), 2 + run
		}
		if len(params) < 5 {
			return Color{}, 0
		}
		return rgbColor(params[2:5]), 5
	}
	return Color{}, 0
}

func rgbColor(p []sgrParam) Color {
	clamp := func(v int) uint8 { return uint8(min(v, 255)) }
	return RGB(clamp(p[0].value), clamp(p[1].value), clamp(p[2].value))
}
