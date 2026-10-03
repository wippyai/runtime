// SPDX-License-Identifier: MPL-2.0

package text

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWidth(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"cjk", "世界", 4},
		{"mixed", "a世b", 4},
		{"hangul", "한글", 4},
		{"combining mark", "é", 1},
		{"precomposed", "é", 1},
		{"combining run", "á̂̃", 1},
		{"emoji", "🙂", 2},
		{"emoji variation selector", "❤️", 2},
		{"zwj family", "👨‍👩‍👧", 2},
		{"flag", "🇺🇸", 2},
		{"skin tone", "👍🏽", 2},
		{"sgr ignored", "\x1b[1;31mred\x1b[0m", 3},
		{"truecolor ignored", "\x1b[38;2;1;2;3mx\x1b[m", 1},
		{"hyperlink ignored", "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\", 5},
		{"bel terminated osc", "\x1b]0;title\x07abc", 3},
		{"controls", "a\tb\r\x00", 2},
		{"unterminated csi", "ok\x1b[31", 2},
		{"unterminated osc", "ok\x1b]52;c;hidden", 2},
		{"invalid utf8", "a\xffb", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Width(tc.in))
		})
	}
}

func TestClusterWidth(t *testing.T) {
	require.Equal(t, 0, ClusterWidth(""))
	require.Equal(t, 1, ClusterWidth("abc"))
	require.Equal(t, 2, ClusterWidth("界x"))
	require.Equal(t, 1, ClusterWidth("éx"))
	require.Equal(t, 2, ClusterWidth("👨‍👩‍👧x"))
}

func TestNextKeepsClustersWhole(t *testing.T) {
	tok := Next("éx")
	require.Equal(t, Cluster, tok.Kind)
	require.Equal(t, "é", tok.Text)

	tok = Next("👨‍👩‍👧!")
	require.Equal(t, "👨‍👩‍👧", tok.Text)

	tok = Next("\x1b[38;2;1;2;3mtext")
	require.Equal(t, CSI, tok.Kind)
	require.Equal(t, "\x1b[38;2;1;2;3m", tok.Text)
	body, ok := tok.SGR()
	require.True(t, ok)
	require.Equal(t, "38;2;1;2;3", body)

	tok = Next("\x1b[2Jx")
	_, ok = tok.SGR()
	require.False(t, ok)

	tok = Next("\x1b]8;id=1;https://example.com\x07x")
	require.Equal(t, OSC, tok.Kind)
	params, url, ok := tok.Hyperlink()
	require.True(t, ok)
	require.Equal(t, "id=1", params)
	require.Equal(t, "https://example.com", url)

	require.Equal(t, Escape, Next("\x1bPpayload\x1b\\after").Kind)
	require.Equal(t, "\x1bPpayload\x1b\\", Next("\x1bPpayload\x1b\\after").Text)
	require.Equal(t, Escape, Next("\x1b7x").Kind)
	require.Equal(t, "\x1b", Next("\x1b").Text)
	require.Equal(t, Control, Next("\u0085x").Kind)
}

func TestStrip(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "plain", "plain"},
		{"sgr", "\x1b[31m界é🙂\x1b[0m", "界é🙂"},
		{"hyperlink", "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\", "label"},
		{"clipboard", "before\x1b]52;c;c2VjcmV0\aafter", "beforeafter"},
		{"dcs", "before\x1bPpayload\x1b\\after", "beforeafter"},
		{"unterminated osc", "before\x1b]52;c;hidden", "before"},
		{"unterminated csi", "ok\x1b[31", "ok"},
		{"keeps line feed and tab", "a\n\tb", "a\n\tb"},
		{"cursor movement", "a\x1b[2;3Hb\x1b[2Kc", "abc"},
		{"short escape", "a\x1b7b\x1b(0c", "abc"},
		{"combining preserved", "é\x1b[m", "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Strip(tc.in))
		})
	}
}

func TestSizeHelpers(t *testing.T) {
	require.Equal(t, 3, Height("a\nb\nc"))
	require.Equal(t, 1, Height(""))
	require.Equal(t, 5, MaxWidth("ab\n\x1b[1m世界x\x1b[m"))
	w, h := Size("hello\nworld!")
	require.Equal(t, 6, w)
	require.Equal(t, 2, h)
}

func BenchmarkWidthASCII(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Width(s)
	}
}
