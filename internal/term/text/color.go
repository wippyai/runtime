// SPDX-License-Identifier: MPL-2.0

package text

import (
	"image/color"
	"strconv"
	"strings"
)

// ColorKind selects how a Color is encoded in SGR.
type ColorKind uint8

const (
	// ColorNone is the terminal default color.
	ColorNone ColorKind = iota
	// ColorBasic is one of the 16 ANSI colors, encoded as 30-37/90-97.
	ColorBasic
	// ColorIndexed is a 256-palette entry, encoded as 38;5;n.
	ColorIndexed
	// ColorRGB is a 24-bit color, encoded as 38;2;r;g;b.
	ColorRGB
)

// Color is a comparable terminal color. The zero value is the default color.
type Color struct {
	kind    ColorKind
	r, g, b uint8
	index   uint8
}

// Basic returns ANSI color n in 0..15.
func Basic(n uint8) Color { return Color{kind: ColorBasic, index: n & 15} }

// Indexed returns palette color n.
func Indexed(n uint8) Color { return Color{kind: ColorIndexed, index: n} }

// RGB returns a 24-bit color.
func RGB(r, g, b uint8) Color { return Color{kind: ColorRGB, r: r, g: g, b: b} }

// Kind reports the color encoding.
func (c Color) Kind() ColorKind { return c.kind }

// IsNone reports whether c is the terminal default color.
func (c Color) IsNone() bool { return c.kind == ColorNone }

// Index returns the palette index of a basic or indexed color.
func (c Color) Index() uint8 { return c.index }

// RGBA implements image/color.Color. The default color reports transparent
// black; palette colors use the xterm palette.
func (c Color) RGBA() (r, g, b, a uint32) {
	var r8, g8, b8 uint8
	switch c.kind {
	case ColorNone:
		return 0, 0, 0, 0
	case ColorBasic, ColorIndexed:
		r8, g8, b8 = paletteRGB(c.index)
	case ColorRGB:
		r8, g8, b8 = c.r, c.g, c.b
	}
	return uint32(r8) * 0x101, uint32(g8) * 0x101, uint32(b8) * 0x101, 0xffff
}

// ColorModel converts any image/color.Color to a Color. Palette identity is
// preserved for Color values; other models become RGB.
func ColorModel(c color.Color) Color {
	if c == nil {
		return Color{}
	}
	if tc, ok := c.(Color); ok {
		return tc
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return Color{}
	}
	if a != 0xffff {
		r, g, b = r*0xffff/a, g*0xffff/a, b*0xffff/a
	}
	return RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// ParseColor reads "#rgb", "#rrggbb" or a palette number 0-255. Numbers below
// 16 are basic ANSI colors.
func ParseColor(s string) (Color, bool) {
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) != 6 {
			return Color{}, false
		}
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return Color{}, false
		}
		return RGB(uint8(v>>16), uint8(v>>8), uint8(v)), true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return Color{}, false
	}
	if n < 16 {
		return Basic(uint8(n)), true
	}
	return Indexed(uint8(n)), true
}

var ansi16 = [16][3]uint8{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func paletteRGB(n uint8) (r, g, b uint8) {
	switch {
	case n < 16:
		c := ansi16[n]
		return c[0], c[1], c[2]
	case n < 232:
		n -= 16
		level := func(v uint8) uint8 {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return level(n / 36), level(n / 6 % 6), level(n % 6)
	default:
		v := 8 + (n-232)*10
		return v, v, v
	}
}

// appendSGR appends the SGR parameters selecting c with base 30 (foreground),
// 40 (background). Underline color uses 58 and the 256-palette forms only.
func (c Color) appendSGR(dst []string, base int) []string {
	switch c.kind {
	case ColorNone:
		return append(dst, strconv.Itoa(base+9))
	case ColorBasic:
		if c.index < 8 {
			return append(dst, strconv.Itoa(base+int(c.index)))
		}
		return append(dst, strconv.Itoa(base+60+int(c.index)-8))
	case ColorIndexed:
		return append(dst, strconv.Itoa(base+8)+";5;"+strconv.Itoa(int(c.index)))
	default:
		return append(dst, strconv.Itoa(base+8)+";2;"+strconv.Itoa(int(c.r))+";"+strconv.Itoa(int(c.g))+";"+strconv.Itoa(int(c.b)))
	}
}

func (c Color) appendUnderlineSGR(dst []string) []string {
	switch c.kind {
	case ColorNone:
		return append(dst, "59")
	case ColorBasic, ColorIndexed:
		return append(dst, "58;5;"+strconv.Itoa(int(c.index)))
	default:
		return append(dst, "58;2;"+strconv.Itoa(int(c.r))+";"+strconv.Itoa(int(c.g))+";"+strconv.Itoa(int(c.b)))
	}
}
