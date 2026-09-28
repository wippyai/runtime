// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestCanvasRowControlsCannotEscapePlacement(t *testing.T) {
	for _, text := range []string{"abc\x1b[2K", "abc\x1b[2J", "abc\t", "abc\x1b[100C", "abc\nnext", "abc\rX"} {
		t.Run(text, func(t *testing.T) {
			c := &canvasWrapper{width: 12, height: 3, screen: &canvasBuffer{Buffer: uv.NewBuffer(12, 3)}}
			for y := range 3 {
				c.put(0, y, "............", 12)
			}
			c.put(3, 1, text, 3)
			for y := range 3 {
				for x := range 12 {
					if y == 1 && x >= 3 && x < 6 {
						continue
					}
					require.Equal(t, ".", c.screen.CellAt(x, y).Content, "cell (%d,%d)", x, y)
				}
			}
			rendered := c.screen.Render()
			require.Equal(t, ansi.Strip(rendered), rendered, "non-styling controls must not reach terminal output")
		})
	}
}

func TestCanvasRegionPreservesStylesAndLinks(t *testing.T) {
	c := &canvasWrapper{width: 10, height: 1, screen: &canvasBuffer{Buffer: uv.NewBuffer(10, 1)}}
	c.put(0, 0, "..........", 10)
	c.put(2, 0, "\x1b[31m\x1b]8;;https://example.com\x1b\\Hi\x1b]8;;\x1b\\\x1b[0m", 2)
	require.Equal(t, "..Hi......", ansi.Strip(c.screen.Render()))
	require.Equal(t, "https://example.com", c.screen.CellAt(2, 0).Link.URL)
	require.NotNil(t, c.screen.CellAt(2, 0).Style.Fg)
	require.True(t, c.screen.CellAt(4, 0).Style.IsZero())
	require.True(t, c.screen.CellAt(4, 0).Link.IsZero())
}

func TestCanvasLuaRowsAndFillDiscardTerminalCommands(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	require.NoError(t, l.DoString(`
  local c = tty.canvas(12, 3)
  assert(c:clear(".\27[2J"))
  assert(c:put_rows(4, 2, {"abc\27[2K", "xyz\27[100C"}, 3))
  local rows = c:rows()
  assert(rows[1] == "............")
  assert(rows[2] == "...abc......")
  assert(rows[3] == "...xyz......")
 `))
}

func BenchmarkCanvasStyledRow(b *testing.B) {
	c := &canvasWrapper{width: 120, height: 1, screen: &canvasBuffer{Buffer: uv.NewBuffer(120, 1)}}
	const text = "\x1b[31magent status\x1b[0m running"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.put(2, 0, text, 80)
	}
}
