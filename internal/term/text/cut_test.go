// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     string
		length int
		tail   string
		want   string
	}{
		{"fits", "hello", 5, "", "hello"},
		{"cut", "hello", 3, "", "hel"},
		{"tail counts", "hello", 4, "…", "hel…"},
		{"tail wider than length", "hello", 0, "…", ""},
		{"wide cluster dropped", "ab世界", 3, "", "ab"},
		{"wide fits", "ab世界", 4, "", "ab世"},
		{"zwj kept whole", "a👨‍👩‍👧b", 3, "", "a👨‍👩‍👧"},
		{"zwj dropped whole", "a👨‍👩‍👧b", 2, "", "a"},
		{"combining stays with base", "aéz", 2, "", "aé"},
		{"sgr kept after cut", "\x1b[31mabcdef\x1b[0m", 3, "", "\x1b[31mabc\x1b[0m"},
		{"sgr between", "\x1b[31mab\x1b[32mcd", 3, "", "\x1b[31mab\x1b[32mc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Truncate(tc.in, tc.length, tc.tail))
		})
	}
}

func TestTruncateStyledTailKeepsWidth(t *testing.T) {
	out := Truncate("\x1b[31mab世界cd\x1b[0m", 7, "…")
	require.LessOrEqual(t, Width(out), 7)
	require.Contains(t, out, "\x1b[31m")
	require.True(t, strings.HasSuffix(out, "\x1b[0m"))
}

func TestCut(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          string
		left, right int
		want        string
	}{
		{"prefix", "abcdef", 0, 3, "abc"},
		{"middle", "abcdef", 2, 4, "cd"},
		{"suffix", "abcdef", 3, 99, "def"},
		{"empty range", "abcdef", 4, 4, ""},
		{"inverted range", "abcdef", 4, 2, ""},
		{"negative left", "abcdef", -3, 2, "ab"},
		{"wide inside", "ab世界cd", 2, 6, "世界"},
		{"wide straddles left", "ab世界cd", 3, 6, "界"},
		{"wide straddles right", "ab世界cd", 2, 5, "世"},
		{"zwj whole", "a👨‍👩‍👧b", 1, 3, "👨‍👩‍👧"},
		{"zwj split drops", "a👨‍👩‍👧b", 2, 4, "b"},
		{"sgr before range kept", "\x1b[31mabcdef", 2, 4, "\x1b[31mcd"},
		{"sgr after range kept", "abcd\x1b[0mef", 0, 2, "ab\x1b[0m"},
		{"controls outside range dropped", "a\nbcd", 2, 4, "cd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Cut(tc.in, tc.left, tc.right))
		})
	}
}

func TestCutPreservesStyleAcrossLeftEdge(t *testing.T) {
	out := Cut("\x1b[31mab世界cd\x1b[0m", 2, 6)
	require.Equal(t, 4, Width(out))
	require.Contains(t, out, "\x1b[31m")
	require.Equal(t, "世界", Strip(out))
}

func TestCutNeverExceedsRange(t *testing.T) {
	in := "a世é🙂‍👩x界bc"
	total := Width(in)
	for left := 0; left <= total; left++ {
		for right := left; right <= total+1; right++ {
			out := Cut(in, left, right)
			require.LessOrEqual(t, Width(out), max(0, right-left), "cut(%d,%d)=%q", left, right, out)
		}
	}
}
