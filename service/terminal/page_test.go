// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"bytes"
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
	vt "github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestPageSurvivesPhysicalPresentation(t *testing.T) {
	for _, synchronized := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "synchronized"}[synchronized], func(t *testing.T) {
			page := ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"}
			renderer := NewPageRenderer(6, page)
			var output bytes.Buffer
			surface := NewSurface(&output, ttyapi.SurfaceOptions{Synchronized: synchronized})
			screen := vt.NewEmulator(6, 2)
			defer screen.Close()
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
					cell := screen.CellAt(x, y)
					if cell.Width == 0 {
						continue
					}
					require.NotNil(t, cell.Style.Bg, "cell (%d,%d)", x, y)
					require.Equal(t, color.RGBAModel.Convert(bg), color.RGBAModel.Convert(cell.Style.Bg), "cell (%d,%d)", x, y)
				}
			}
			require.Equal(t, "f", screen.CellAt(5, 0).Content)
			require.Equal(t, "X", screen.CellAt(5, 1).Content)
			paint([]string{renderer.RenderRow("x")})
			require.Equal(t, "x", screen.CellAt(0, 0).Content)
			for x := range 6 {
				require.Equal(t, " ", screen.CellAt(x, 1).Content)
			}
			paint(rows)
			require.Equal(t, "f", screen.CellAt(5, 0).Content, "full-width repaint must not scroll")
		})
	}
}

func TestPageRendererClipsWideCellsAndKeepsPaintedBlanks(t *testing.T) {
	r := NewPageRenderer(3, ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"})
	require.Equal(t, "a界", ansi.Strip(r.RenderRow("a界"))) // whole wide cell fits
	require.Equal(t, "ab ", ansi.Strip(r.RenderRow("ab界")))
	require.Equal(t, "   ", ansi.Strip(r.RenderRow("")))
}
