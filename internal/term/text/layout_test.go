// SPDX-License-Identifier: MPL-2.0

package text

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrap(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"fits", "hello world", 11, "hello world"},
		{"breaks at space", "hello world", 8, "hello\nworld"},
		{"greedy fill", "aa bb cc dd", 5, "aa bb\ncc dd"},
		{"long word hard break", "abcdefgh", 3, "abc\ndef\ngh"},
		{"wide word hard break", "世界世界", 4, "世界\n世界"},
		{"keeps existing newlines", "a b\nc d", 3, "a b\nc d"},
		{"zero limit", "a b", 0, "a b"},
		{"trailing spaces dropped at break", "ab   cd", 4, "ab\ncd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Wrap(tc.in, tc.limit))
		})
	}
}

func TestWrapCarriesStyleAcrossBreaks(t *testing.T) {
	out := Wrap("\x1b[31mhello world\x1b[0m", 6)
	require.Equal(t, "\x1b[31mhello\x1b[m\n\x1b[31mworld\x1b[0m", out)
	require.Equal(t, "hello\nworld", Strip(out))
	for _, line := range []string{"hello", "world"} {
		require.LessOrEqual(t, Width(line), 6)
	}
}

func TestJoinHorizontal(t *testing.T) {
	require.Equal(t, "", JoinHorizontal(Top))
	require.Equal(t, "solo", JoinHorizontal(Top, "solo"))
	require.Equal(t, "ABCD", JoinHorizontal(Top, "AB", "CD"))
	require.Equal(t, "a x\n  y", JoinHorizontal(Top, "a ", "x\ny"))
	require.Equal(t, "  x\na y", JoinHorizontal(Bottom, "a ", "x\ny"))
	require.Equal(t, "世 x\n   y", JoinHorizontal(Top, "世 ", "x\ny"), "wide cells pad by display width")
}

func TestJoinVertical(t *testing.T) {
	require.Equal(t, "top\nbottom", JoinVertical(Left, "top\nbottom"))
	require.Equal(t, "ab \nxyz", JoinVertical(Left, "ab", "xyz"))
	require.Equal(t, " ab\nxyz", JoinVertical(Right, "ab", "xyz"))
	require.Equal(t, " a \n b \nxyz", JoinVertical(Center, "a\nb", "xyz"))
}

func TestPlace(t *testing.T) {
	require.Equal(t, "ab    ", PlaceHorizontal(6, Left, "ab"))
	require.Equal(t, "    ab", PlaceHorizontal(6, Right, "ab"))
	require.Equal(t, "  ab  ", PlaceHorizontal(6, Center, "ab"))
	require.Equal(t, "toolong", PlaceHorizontal(3, Center, "toolong"))
	require.Equal(t, "x\n \n ", PlaceVertical(3, Top, "x"))
	require.Equal(t, " \n \nx", PlaceVertical(3, Bottom, "x"))
	require.Equal(t, " \nx\n ", PlaceVertical(3, Center, "x"))
	require.Equal(t, "    \n ab \n    ", Place(4, 3, Center, Center, "ab"))
}
