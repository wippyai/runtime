// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestCanvasClipsANSIAsCellsWithoutControlSequenceAmplification(t *testing.T) {
	canvas := &canvasWrapper{
		width: 12, height: 1,
		screen: &canvasBuffer{Buffer: uv.NewBuffer(12, 1)},
	}
	canvas.put(0, 0, strings.Repeat(".", 12), 12)
	denseTail := "\x1b[31mabcdefghij" + strings.Repeat("\x1b[38;2;1;2;3m", 1000)
	canvas.put(3, 0, denseTail, 4)
	rendered := canvas.screen.Render()

	require.Equal(t, 12, ansi.StringWidth(rendered))
	require.Contains(t, ansi.Strip(rendered), "...abcd.....")
	require.Less(t, len(rendered), 128, "discarded ANSI state must not survive composition")
}

func TestCanvasClipsNegativeOrigin(t *testing.T) {
	canvas := &canvasWrapper{
		width: 4, height: 1,
		screen: &canvasBuffer{Buffer: uv.NewBuffer(4, 1)},
	}
	canvas.put(-2, 0, "abcdef", 6)
	require.Equal(t, "cdef", ansi.Strip(canvas.screen.Render()))
}

func TestCanvasRejectsExcessiveAreaBeforeAllocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`tty.canvas(16384, 16384)`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "canvas area exceeds allocation limit")
}

func TestCanvasPutRowsClipsAndValidatesRows(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	require.NoError(t, l.DoString(`
		local canvas = tty.canvas(4, 3)
		assert(canvas:clear("."))
		assert(canvas:put_rows(2, 0, {"ABCD", "EFGH", "IJKL", "MNOP"}, 3))
		local rows = canvas:rows()
		assert(rows[1] == ".EFG")
		assert(rows[2] == ".IJK")
		assert(rows[3] == ".MNO")

		assert(canvas:clear("."))
		local ok = pcall(function()
			canvas:put_rows(1, 1, {"valid", 42})
		end)
		assert(not ok)
		for _, row in ipairs(canvas:rows()) do assert(row == "....") end
	`))
}
