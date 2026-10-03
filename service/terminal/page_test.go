// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"bytes"
	"strings"
	"testing"

	xterm "github.com/gitpod-io/xterm-go"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/internal/term/text"
)

func screenCell(screen *xterm.Terminal, x, y int) *xterm.CellData {
	cell := xterm.NewCellData()
	buf := screen.Buffer()
	buf.Lines.Get(buf.YBase+y).LoadCell(x, cell)
	return cell
}

func TestPageSurvivesPhysicalPresentation(t *testing.T) {
	for _, synchronized := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "synchronized"}[synchronized], func(t *testing.T) {
			page := ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"}
			renderer := NewPageRenderer(6, page)
			var output bytes.Buffer
			surface := NewSurface(&output, ttyapi.SurfaceOptions{Synchronized: synchronized})
			screen := xterm.New(xterm.WithCols(6), xterm.WithRows(2))
			paint := func(rows []string) {
				output.Reset()
				_, err := surface.Present(ttyapi.Frame{Rows: rows})
				require.NoError(t, err)
				_, err = screen.Write(output.Bytes())
				require.NoError(t, err)
			}
			rows := []string{renderer.RenderRow("abcdef"), renderer.RenderRow("世界 X")}
			paint(rows)
			_, bg := page.Colors()
			for y := range 2 {
				for x := range 6 {
					cell := screenCell(screen, x, y)
					if cell.GetWidth() == 0 {
						continue
					}
					require.True(t, cell.IsBgRGB(), "cell (%d,%d)", x, y)
					wantR, wantG, wantB, _ := bg.RGBA()
					require.Equal(t, int(wantR>>8)<<16|int(wantG>>8)<<8|int(wantB>>8), cell.GetBgColor(), "cell (%d,%d)", x, y)
				}
			}
			require.Equal(t, "f", screenCell(screen, 5, 0).GetChars())
			require.Equal(t, "X", screenCell(screen, 5, 1).GetChars())
			paint([]string{renderer.RenderRow("x")})
			require.Equal(t, "x", screenCell(screen, 0, 0).GetChars())
			for x := range 6 {
				require.Empty(t, strings.TrimSpace(screenCell(screen, x, 1).GetChars()))
			}
			paint(rows)
			require.Equal(t, "f", screenCell(screen, 5, 0).GetChars(), "full-width repaint must not scroll")
		})
	}
}

func TestPageRendererClipsWideCellsAndKeepsPaintedBlanks(t *testing.T) {
	r := NewPageRenderer(3, ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"})
	require.Equal(t, "a界", text.Strip(r.RenderRow("a界"))) // whole wide cell fits
	require.Equal(t, "ab ", text.Strip(r.RenderRow("ab界")))
	require.Equal(t, "   ", text.Strip(r.RenderRow("")))
}
