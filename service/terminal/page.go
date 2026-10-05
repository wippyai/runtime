// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/canvas"
	"github.com/wippyai/tty/text"
)

// PageRenderer resolves default colors at the styled-cell boundary. One
// bounded scratch row is reused; callers retain only rendered row strings.
// It is confined to its owning viewport's presentation lock.
type PageRenderer struct {
	row   *canvas.Buffer
	blank canvas.Cell
}

func NewPageRenderer(width int, page ttyapi.Page) *PageRenderer {
	fg, bg := page.Colors()
	return &PageRenderer{
		row: canvas.NewBuffer(width, 1),
		blank: canvas.Cell{Content: " ", Width: 1, Style: text.Style{
			Fg: text.ColorModel(fg), Bg: text.ColorModel(bg),
		}},
	}
}

// RenderRow decodes a styled row into cells, fills default colors and renders
// the full viewport width. Control-only sequences produce no cells and a wide
// cell that does not fit the last column becomes a styled blank.
func (r *PageRenderer) RenderRow(row string) string {
	width := r.row.Width()
	for x := range width {
		r.row.Set(x, 0, &r.blank)
	}
	canvas.Decode(row, func(col int, cell canvas.Cell) bool {
		if col >= width {
			return false
		}
		if cell.Style.Fg.IsNone() {
			cell.Style.Fg = r.blank.Style.Fg
		}
		if cell.Style.Bg.IsNone() {
			cell.Style.Bg = r.blank.Style.Bg
		}
		r.row.Set(col, 0, &cell)
		return true
	})
	return r.row.Render()
}

// Width returns the viewport width in cells.
func (r *PageRenderer) Width() int { return r.row.Width() }
