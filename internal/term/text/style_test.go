// SPDX-License-Identifier: MPL-2.0

package text

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func apply(body string) Style {
	var s Style
	s.ApplySGR(body)
	return s
}

func TestApplySGR(t *testing.T) {
	require.Equal(t, Style{Attrs: AttrBold | AttrItalic, Fg: Basic(1)}, apply("1;3;31"))
	require.Equal(t, Style{Fg: Basic(9), Bg: Basic(12)}, apply("91;104"))
	require.Equal(t, Style{Fg: Indexed(200)}, apply("38;5;200"))
	require.Equal(t, Style{Bg: RGB(1, 2, 3)}, apply("48;2;1;2;3"))
	require.Equal(t, Style{Fg: RGB(1, 2, 3)}, apply("38:2:1:2:3"))
	require.Equal(t, Style{Fg: RGB(1, 2, 3)}, apply("38:2::1:2:3"))
	require.Equal(t, Style{Fg: Indexed(7)}, apply("38:5:7"))
	require.Equal(t, Style{Underline: UnderlineCurly}, apply("4:3"))
	require.Equal(t, Style{Underline: UnderlineSingle}, apply("4"))
	require.Equal(t, Style{UnderlineColor: RGB(9, 8, 7), Underline: UnderlineSingle}, apply("4;58;2;9;8;7"))
	require.True(t, apply("1;31;0").IsZero())
	require.True(t, apply("").IsZero())
	require.True(t, apply("1;3;4;5;7;9;22;23;24;25;27;29").Attrs == 0)
	require.Equal(t, Style{Bg: Basic(2)}, apply("31;32;39;42"))
	require.Equal(t, Style{}, apply("38"), "a color selector without a color is ignored")
}

func TestStyleString(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Style
		want string
	}{
		{"zero", Style{}, "\x1b[m"},
		{"attrs", Style{Attrs: AttrBold | AttrFaint | AttrItalic | AttrBlink | AttrReverse | AttrStrikethrough}, "\x1b[1;2;3;5;7;9m"},
		{"basic fg bg", Style{Fg: Basic(1), Bg: Basic(2)}, "\x1b[31;42m"},
		{"bright", Style{Fg: Basic(9), Bg: Basic(15)}, "\x1b[91;107m"},
		{"indexed", Style{Fg: Indexed(200), Bg: Indexed(16)}, "\x1b[38;5;200;48;5;16m"},
		{"truecolor", Style{Fg: RGB(1, 2, 3), Bg: RGB(4, 5, 6)}, "\x1b[38;2;1;2;3;48;2;4;5;6m"},
		{"underline styles", Style{Underline: UnderlineDashed}, "\x1b[4:5m"},
		{"single underline", Style{Underline: UnderlineSingle}, "\x1b[4m"},
		{"underline color", Style{Underline: UnderlineSingle, UnderlineColor: RGB(1, 2, 3)}, "\x1b[4;58;2;1;2;3m"},
		{"rapid blink conceal", Style{Attrs: AttrRapidBlink | AttrConceal}, "\x1b[6;8m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.in.String())
		})
	}
	require.Equal(t, "\x1b[1mx\x1b[m", Style{Attrs: AttrBold}.Styled("x"))
	require.Equal(t, "x", Style{}.Styled("x"))
}

func TestStyleRoundTrip(t *testing.T) {
	for _, in := range []Style{
		{Attrs: AttrBold | AttrItalic | AttrStrikethrough, Fg: RGB(10, 20, 30), Bg: Indexed(99)},
		{Fg: Basic(3), Bg: Basic(14), Underline: UnderlineDouble, UnderlineColor: Indexed(5)},
		{Attrs: AttrFaint | AttrBlink | AttrReverse, Underline: UnderlineSingle},
	} {
		require.Equal(t, in, apply(in.String()[2:len(in.String())-1]))
	}
}

func TestStyleDiffTransitions(t *testing.T) {
	styles := []Style{
		{},
		{Attrs: AttrBold},
		{Attrs: AttrBold | AttrFaint, Fg: Basic(1)},
		{Attrs: AttrItalic, Fg: RGB(1, 2, 3), Bg: Indexed(40)},
		{Underline: UnderlineSingle, Attrs: AttrReverse},
		{Underline: UnderlineCurly, UnderlineColor: RGB(5, 5, 5), Attrs: AttrStrikethrough | AttrBlink},
		{Fg: Basic(12), Bg: Basic(2), Attrs: AttrRapidBlink | AttrConceal},
	}
	for _, from := range styles {
		for _, to := range styles {
			pen := from
			if seq := to.Diff(from); seq != "" {
				pen.ApplySGR(seq[2 : len(seq)-1])
			}
			require.Equal(t, to, pen, "diff %q -> %q", from.String(), to.String())
		}
	}
	require.Equal(t, "", Style{Attrs: AttrBold}.Diff(Style{Attrs: AttrBold}))
	require.Equal(t, "\x1b[m", Style{}.Diff(Style{Attrs: AttrBold}))
	require.Equal(t, "\x1b[31m", Style{Fg: Basic(1)}.Diff(Style{}))
	require.Equal(t, "\x1b[39m", Style{Bg: Basic(2)}.Diff(Style{Fg: Basic(1), Bg: Basic(2)}))
}

func TestParseColor(t *testing.T) {
	c, ok := ParseColor("#FF8000")
	require.True(t, ok)
	require.Equal(t, RGB(255, 128, 0), c)
	c, ok = ParseColor("#f80")
	require.True(t, ok)
	require.Equal(t, RGB(255, 136, 0), c)
	c, ok = ParseColor("5")
	require.True(t, ok)
	require.Equal(t, Basic(5), c)
	c, ok = ParseColor("200")
	require.True(t, ok)
	require.Equal(t, Indexed(200), c)
	for _, bad := range []string{"", "#12", "#gggggg", "256", "-1", "red"} {
		_, ok = ParseColor(bad)
		require.False(t, ok, bad)
	}
}

func TestColorRGBA(t *testing.T) {
	r, g, b, a := RGB(255, 0, 128).RGBA()
	require.Equal(t, [4]uint32{0xffff, 0, 0x8080, 0xffff}, [4]uint32{r, g, b, a})
	r, g, b, _ = Indexed(196).RGBA()
	require.Equal(t, [3]uint32{0xffff, 0, 0}, [3]uint32{r, g, b})
	_, _, _, a = Color{}.RGBA()
	require.Zero(t, a)
	require.Equal(t, RGB(1, 2, 3), ColorModel(RGB(1, 2, 3)))
	require.Equal(t, Color{}, ColorModel(nil))
}

func TestHyperlinkSequences(t *testing.T) {
	require.Equal(t, "\x1b]8;id=1;https://x\x07", SetHyperlink("https://x", "id=1"))
	require.Equal(t, "\x1b]8;;\x07", ResetHyperlink())
	params, url, ok := Next(SetHyperlink("https://x", "a=1", "b=2")).Hyperlink()
	require.True(t, ok)
	require.Equal(t, "a=1:b=2", params)
	require.Equal(t, "https://x", url)
}
