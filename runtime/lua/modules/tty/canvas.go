// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	canvasTypeName   = "tty.Canvas"
	maxCanvasColumns = 16384
	maxCanvasCells   = 1 << 18
)

// canvasWrapper is a bounded styled-cell compositor. ANSI strings are parsed
// once at the placement boundary, so clipped control sequences cannot leak or
// multiply as independently rendered surfaces overlap.
type canvasWrapper struct {
	screen *canvasBuffer
	region canvasRegion
	width  int
	height int
}

type canvasBuffer struct{ *uv.Buffer }

func (b *canvasBuffer) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func init() {
	value.RegisterTypeMethods(nil, canvasTypeName,
		map[string]lua.LGoFunc{"__tostring": canvasToString},
		map[string]lua.LGoFunc{
			"clear":    canvasClear,
			"put":      canvasPut,
			"put_rows": canvasPutRows,
			"rows":     canvasRows,
		})
}

func ttyCanvasNew(l *lua.LState) int {
	width, ok := integerValue(l.Get(1))
	if !ok {
		l.ArgError(1, "canvas width must be an integer")
		return 0
	}
	height, ok := integerValue(l.Get(2))
	if !ok {
		l.ArgError(2, "canvas height must be an integer")
		return 0
	}
	if width < 1 || width > maxCanvasColumns {
		l.ArgError(1, "canvas width must be positive and bounded")
		return 0
	}
	if height < 1 || height > maxSurfaceRows {
		l.ArgError(2, "canvas height must be positive and bounded")
		return 0
	}
	if height > maxCanvasCells/width {
		l.ArgError(2, "canvas area exceeds allocation limit")
		return 0
	}
	c := &canvasWrapper{
		width: width, height: height,
		screen: &canvasBuffer{Buffer: uv.NewBuffer(width, height)},
	}
	value.PushTypedUserData(l, c, canvasTypeName)
	return 1
}

func checkCanvas(l *lua.LState) *canvasWrapper {
	ud := l.CheckUserData(1)
	if canvas, ok := ud.Value.(*canvasWrapper); ok {
		return canvas
	}
	l.ArgError(1, "tty.Canvas expected")
	return nil
}

func canvasToString(l *lua.LState) int {
	_ = checkCanvas(l)
	l.Push(lua.LString("tty.Canvas{}"))
	return 1
}

func canvasClear(l *lua.LState) int {
	c := checkCanvas(l)
	c.screen.Clear()
	fill := l.OptString(2, "")
	if fill != "" {
		fillWidth := ansi.StringWidth(fill)
		if fillWidth > 0 {
			fill = strings.Repeat(fill, (c.width+fillWidth-1)/fillWidth)
		}
		// Decode one complete fill row, then copy its styled cells. StyledString
		// uses the bounded shared ANSI parser pool and understands SGR and OSC 8.
		row := &canvasBuffer{Buffer: uv.NewBuffer(c.width, 1)}
		region := &canvasRegion{canvasBuffer: row, area: row.Bounds()}
		uv.NewStyledString(ansi.Cut(fill, 0, c.width)).Draw(region, region.Bounds())
		for y := 0; y < c.height; y++ {
			for x := 0; x < c.width; x++ {
				c.screen.SetCell(x, y, row.CellAt(x, 0))
			}
		}
	}
	l.Push(lua.LTrue)
	return 1
}

func canvasPut(l *lua.LState) int {
	c := checkCanvas(l)
	x := canvasCoordinate(l, 2, "x")
	y := canvasCoordinate(l, 3, "y")
	x, y = x-1, y-1
	text := l.CheckString(4)
	limit := c.width
	if l.GetTop() >= 5 {
		var ok bool
		limit, ok = integerValue(l.Get(5))
		if !ok || limit < 0 || limit > maxTerminalDimension {
			l.ArgError(5, "canvas width must be a bounded non-negative integer")
			return 0
		}
	}
	if limit > 0 && y >= 0 && y < c.height {
		c.put(x, y, text, limit)
	}
	l.Push(lua.LTrue)
	return 1
}

func canvasPutRows(l *lua.LState) int {
	c := checkCanvas(l)
	x := canvasCoordinate(l, 2, "x")
	y := canvasCoordinate(l, 3, "y")
	x, y = x-1, y-1
	rows := l.CheckTable(4)
	rowCount := rows.Len()
	if rowCount > maxSurfaceRows {
		l.ArgError(4, "row count exceeds limit")
		return 0
	}
	limit := c.width
	if l.GetTop() >= 5 {
		var ok bool
		limit, ok = integerValue(l.Get(5))
		if !ok || limit < 0 || limit > maxTerminalDimension {
			l.ArgError(5, "canvas width must be a bounded non-negative integer")
			return 0
		}
	}
	for index := 1; index <= rowCount; index++ {
		if rows.RawGetInt(index).Type() != lua.LTString {
			l.ArgError(4, fmt.Sprintf("row %d must be a string", index))
			return 0
		}
	}
	for index := 1; index <= rowCount; index++ {
		row := rows.RawGetInt(index)
		rowY := y + index - 1
		if rowY >= 0 && rowY < c.height {
			c.put(x, rowY, string(row.(lua.LString)), limit)
		}
	}
	l.Push(lua.LTrue)
	return 1
}

func canvasCoordinate(l *lua.LState, index int, name string) int {
	coordinate, ok := integerValue(l.Get(index))
	if !ok || coordinate < -maxTerminalDimension || coordinate > maxTerminalDimension {
		l.ArgError(index, fmt.Sprintf("canvas %s must be a bounded integer", name))
		return 0
	}
	return coordinate
}

func (c *canvasWrapper) put(x, y int, text string, limit int) {
	if x >= c.width {
		return
	}
	sourceX := 0
	if x < 0 {
		sourceX, x = -x, 0
		limit -= sourceX
	}
	available := min(limit, c.width-x)
	textWidth := ansi.StringWidth(text)
	covered := min(available, textWidth-sourceX)
	if covered <= 0 {
		return
	}
	// ansi.Cut may retain discarded trailing control sequences by design. The
	// drawing target independently rejects control-only cells and writes outside
	// the placement rectangle, including decoder tail writes.
	clipped := ansi.Cut(text, sourceX, sourceX+covered)
	// Canvas is owned by one Lua state. Reuse its drawing target rather than
	// allocating a region for every row in an animated frame.
	c.region.canvasBuffer, c.region.area = c.screen, uv.Rect(x, y, covered, 1)
	uv.NewStyledString(clipped).Draw(&c.region, c.region.Bounds())
}

func canvasRows(l *lua.LState) int {
	c := checkCanvas(l)
	rows := strings.Split(c.screen.Render(), "\n")
	result := l.CreateTable(c.height, 0)
	for index := 0; index < c.height; index++ {
		row := ""
		if index < len(rows) {
			row = rows[index]
		}
		result.RawSetInt(index+1, lua.LString(row))
	}
	l.Push(result)
	return 1
}
