// SPDX-License-Identifier: MPL-2.0

package canvas

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/internal/term/text"
)

func collect(row string) (cols []int, cells []Cell) {
	Decode(row, func(col int, c Cell) bool {
		cols = append(cols, col)
		cells = append(cells, c)
		return true
	})
	return
}

func TestDecodeCarriesStyleAndLink(t *testing.T) {
	cols, cells := collect("\x1b[31;1ma\x1b]8;id=1;https://x\x07b\x1b]8;;\x07\x1b[0mc")
	require.Equal(t, []int{0, 1, 2}, cols)
	require.Equal(t, text.Style{Fg: text.Basic(1), Attrs: text.AttrBold}, cells[0].Style)
	require.Equal(t, Link{}, cells[0].Link)
	require.Equal(t, Link{Params: "id=1", URL: "https://x"}, cells[1].Link)
	require.Equal(t, text.Style{Fg: text.Basic(1), Attrs: text.AttrBold}, cells[1].Style)
	require.True(t, cells[2].Style.IsZero())
	require.True(t, cells[2].Link.IsZero())
}

func TestDecodeWideAndCombining(t *testing.T) {
	cols, cells := collect("a世é🙂👨‍👩‍👧")
	require.Equal(t, []int{0, 1, 3, 4, 6}, cols)
	require.Equal(t, 2, cells[1].Width)
	require.Equal(t, "é", cells[2].Content)
	require.Equal(t, 2, cells[4].Width)
}

func TestDecodeDropsControlsAndStopsAtLineFeed(t *testing.T) {
	for in, want := range map[string]string{
		"abc\x1b[2K":               "abc",
		"abc\x1b[2J":               "abc",
		"abc\t":                    "abc",
		"a\x1b[100Cb":              "ab",
		"abc\rX":                   "abcX",
		"abc\nnext":                "abc",
		"a\x1b]52;c;c2VjcmV0\x07b": "ab",
		"a\x1bPpayload\x1b\\b":     "ab",
		"a\x1b[31":                 "a",
	} {
		_, cells := collect(in)
		var got strings.Builder
		for _, c := range cells {
			got.WriteString(c.Content)
		}
		require.Equal(t, want, got.String(), "%q", in)
	}
}

func TestDecodeStopsWhenAsked(t *testing.T) {
	n := 0
	Decode("abcdef", func(int, Cell) bool { n++; return n < 3 })
	require.Equal(t, 3, n)
}

func TestLineSetRepairsWideCells(t *testing.T) {
	l := Parse("世界", 4)
	require.Equal(t, "世", l[0].Content)
	require.Zero(t, l[1].Width)

	l.Set(1, &Cell{Content: "x", Width: 1})
	require.Equal(t, " ", l[0].Content, "overwriting the placeholder blanks the wide cell")
	require.Equal(t, "x", l[1].Content)

	l = Parse("世界", 4)
	l.Set(0, &Cell{Content: "y", Width: 1})
	require.Equal(t, "y", l[0].Content)
	require.Equal(t, " ", l[1].Content, "overwriting the head blanks the tail")
	require.Equal(t, "界", l[2].Content)
}

func TestLineSetClipsWideCellAtEdge(t *testing.T) {
	l := NewBuffer(3, 1).Line(0)
	l.Set(2, &Cell{Content: "世", Width: 2, Style: text.Style{Fg: text.Basic(1)}})
	require.Equal(t, " ", l[2].Content)
	require.Equal(t, text.Basic(1), l[2].Style.Fg, "the blank keeps the cell style")
}

func TestBufferDrawRowWindow(t *testing.T) {
	b := NewBuffer(8, 1)
	b.DrawRow(0, 0, 8, 0, "........")
	b.DrawRow(2, 0, 3, 0, "abcdef")
	require.Equal(t, "..abc...", text.Strip(b.Render()))

	b.DrawRow(0, 0, 8, 0, "........")
	b.DrawRow(1, 0, 4, 2, "abcdef")
	require.Equal(t, ".cdef...", text.Strip(b.Render()))
}

func TestBufferDrawRowClearsWindowAndDropsStraddlingCells(t *testing.T) {
	b := NewBuffer(6, 1)
	b.DrawRow(0, 0, 6, 0, "......")
	b.DrawRow(1, 0, 3, 0, "a世界")
	require.Equal(t, ".a世..", text.Strip(b.Render()))
}

func TestBufferDrawRowSkipDropsStraddlingCell(t *testing.T) {
	b := NewBuffer(4, 1)
	b.DrawRow(0, 0, 4, 1, "世界")
	require.Equal(t, " 界", b.Render(), "columns stay aligned to the source")
}

func TestBufferDrawRowClipsToBuffer(t *testing.T) {
	b := NewBuffer(4, 1)
	b.DrawRow(2, 0, 10, 0, "abcdef")
	require.Equal(t, "  ab", b.Render())
	b.DrawRow(9, 0, 3, 0, "zzz")
	b.DrawRow(-1, 0, 3, 0, "zzz")
	require.Equal(t, 4, b.Width())
}

func TestRenderOmitsTrailingBlanksAndPreservesLeading(t *testing.T) {
	b := NewBuffer(6, 1)
	b.DrawRow(1, 0, 2, 0, "ab")
	require.Equal(t, " ab", b.Render())
}

func TestRenderMinimalSequences(t *testing.T) {
	b := NewBuffer(6, 1)
	b.DrawRow(0, 0, 6, 0, "\x1b[31mab\x1b[1mcd\x1b[0mef")
	require.Equal(t, "\x1b[31mab\x1b[1mcd\x1b[mef", b.Render())

	b = NewBuffer(4, 1)
	b.DrawRow(0, 0, 4, 0, "\x1b[31mabcd")
	require.Equal(t, "\x1b[31mabcd\x1b[m", b.Render())

	b = NewBuffer(4, 1)
	b.DrawRow(0, 0, 4, 0, "\x1b]8;;https://x\x07ab\x1b]8;;\x07cd")
	require.Equal(t, "\x1b]8;;https://x\x07ab\x1b]8;;\x07cd", b.Render())
}

func TestRenderRoundTripsThroughParse(t *testing.T) {
	row := "\x1b[1;38;2;1;2;3mhello\x1b[0m \x1b[4;48;5;200m世界\x1b[m!"
	line := Parse(row, 14)
	again := Parse(line.Render(), 14)
	require.Equal(t, line, again)
}

func TestRenderRowsJoinedByLineFeed(t *testing.T) {
	b := NewBuffer(3, 2)
	b.DrawRow(0, 0, 3, 0, "abc")
	b.DrawRow(0, 1, 3, 0, "xyz")
	require.Equal(t, "abc\nxyz", b.Render())
}

func TestFillRowsAndClear(t *testing.T) {
	b := NewBuffer(4, 2)
	row := NewBuffer(4, 1)
	row.DrawRow(0, 0, 4, 0, "ab")
	b.FillRows(row.Line(0))
	require.Equal(t, "ab\nab", b.Render())
	b.Clear()
	require.Equal(t, "\n", b.Render())
	require.Nil(t, b.At(4, 0))
	require.Nil(t, b.At(0, 2))
	require.Nil(t, b.Line(-1))
}

func BenchmarkDrawStyledRow(b *testing.B) {
	buf := NewBuffer(120, 1)
	const row = "\x1b[31magent status\x1b[0m running"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.DrawRow(2, 0, 80, 0, row)
	}
}
