// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/text"
	"github.com/wippyai/tty/vt"
	"github.com/wippyai/tty/vt/screen"
)

func screenCell(term *vt.Terminal, x, y int) screen.Cell {
	return term.Screen().Line(y)[x]
}

func TestPageSurvivesPhysicalPresentation(t *testing.T) {
	for _, synchronized := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "synchronized"}[synchronized], func(t *testing.T) {
			page := ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"}
			renderer := NewPageRenderer(6, page)
			var output bytes.Buffer
			surface := NewSurface(&output, ttyapi.SurfaceOptions{Synchronized: synchronized})
			screen := vt.New(vt.Options{Cols: 6, Rows: 2})
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
			wantBg := text.ColorModel(bg)
			for y := range 2 {
				for x := range 6 {
					cell := screenCell(screen, x, y)
					if x > 0 && screenCell(screen, x-1, y).Wide {
						continue
					}
					require.Equal(t, wantBg, cell.Style.Bg, "cell (%d,%d)", x, y)
				}
			}
			require.Equal(t, "f", screenCell(screen, 5, 0).Cluster)
			require.Equal(t, "X", screenCell(screen, 5, 1).Cluster)
			paint([]string{renderer.RenderRow("x")})
			require.Equal(t, "x", screenCell(screen, 0, 0).Cluster)
			for x := range 6 {
				require.Empty(t, strings.TrimSpace(screenCell(screen, x, 1).Cluster))
			}
			paint(rows)
			require.Equal(t, "f", screenCell(screen, 5, 0).Cluster, "full-width repaint must not scroll")
		})
	}
}

func TestPageRendererClipsWideCellsAndKeepsPaintedBlanks(t *testing.T) {
	r := NewPageRenderer(3, ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"})
	require.Equal(t, "a界", text.Strip(r.RenderRow("a界"))) // whole wide cell fits
	require.Equal(t, "ab ", text.Strip(r.RenderRow("ab界")))
	require.Equal(t, "   ", text.Strip(r.RenderRow("")))
}
