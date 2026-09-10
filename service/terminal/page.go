// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// PageRenderer resolves default colors at the styled-cell boundary. One
// bounded scratch row is reused; callers retain only rendered row strings.
// It is confined to its owning viewport's presentation lock.
type PageRenderer struct {
	*uv.Buffer
	foreground color.Color
	background color.Color
}

func NewPageRenderer(width int, page ttyapi.Page) *PageRenderer {
	fg, bg := page.Colors()
	return &PageRenderer{Buffer: uv.NewBuffer(width, 1), foreground: fg, background: bg}
}

func (r *PageRenderer) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (r *PageRenderer) SetCell(x, y int, cell *uv.Cell) {
	if y != 0 || x < 0 || x >= r.Width() || (cell != nil && cell.Width <= 0) {
		return
	}
	c := uv.EmptyCell
	if cell != nil {
		c = *cell
	}
	if c.Style.Fg == nil {
		c.Style.Fg = r.foreground
	}
	if c.Style.Bg == nil {
		c.Style.Bg = r.background
	}
	if x+c.Width > r.Width() {
		c.Empty()
		for column := x; column < r.Width(); column++ {
			r.Buffer.SetCell(column, y, &c)
		}
		return
	}
	r.Buffer.SetCell(x, y, &c)
}

func (r *PageRenderer) RenderRow(row string) string {
	// StyledString parses SGR, resets and hyperlinks into cells. Control-only
	// decoder tails are rejected by SetCell, and wide cells clip to styled blanks.
	uv.NewStyledString(row).Draw(r, r.Bounds())
	return r.Render()
}
