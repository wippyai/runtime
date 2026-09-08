// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	relaysys "github.com/wippyai/runtime/system/relay"
	ttysys "github.com/wippyai/runtime/system/tty"
)

const (
	luaPageForeground = "#e0def4"
	luaPageBackground = "#191724"
)

type viewportInbox struct{ packages chan *relay.Package }

func (i *viewportInbox) Send(*relay.Package) error { return nil }

// viewportProcessContext wires the real viewport broker so Lua sees the rows a
// producer publishes rather than a fixture.
func viewportProcessContext(t *testing.T, service *ttysys.Service, id string) context.Context {
	t.Helper()
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), service)
	node := relaysys.NewNode("node")
	require.NoError(t, node.RegisterHost("workers", &viewportInbox{packages: make(chan *relay.Package, 1)}))
	ctx = relay.WithNode(ctx, node)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { _ = frame.Close() })
	require.NoError(t, frame.Set(runtime.FramePIDKey, pid.PID{Node: "node", Host: "workers", UniqID: id}))
	return ctx
}

func luaViewportState(t *testing.T, service *ttysys.Service) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindTTY(l)
	l.SetContext(viewportProcessContext(t, service, "shell"))
	return l
}

// present drives the producer side of a viewport created from Lua.
func present(t *testing.T, service *ttysys.Service, grant string, rows []string) {
	t.Helper()
	binding, err := service.Binding(grant)
	require.NoError(t, err)
	port, err := binding.Resolve(viewportProcessContext(t, service, "producer"))
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	_, err = surface.Present(ttyapi.Frame{Rows: rows})
	require.NoError(t, err)
}

func luaString(t *testing.T, l *lua.LState, name string) string {
	t.Helper()
	value, ok := l.GetGlobal(name).(lua.LString)
	require.Truef(t, ok, "global %s must be a string", name)
	return string(value)
}

// luaCells decodes a snapshot row back into styled cells.
func luaCells(t *testing.T, row string, width int) uv.Line {
	t.Helper()
	screen := uv.NewScreenBuffer(width, 1)
	screen.Method = ansi.GraphemeWidth
	uv.NewStyledString(row).Draw(screen, screen.Bounds())
	return screen.Line(0)
}

func luaSameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

func luaPageColors(t *testing.T) (color.Color, color.Color) {
	t.Helper()
	foreground, background := (ttyapi.Page{Foreground: luaPageForeground, Background: luaPageBackground}).Colors()
	return foreground, background
}

func requireLuaPageRow(t *testing.T, row string, width int, foreground, background color.Color) {
	t.Helper()
	require.Equal(t, width, ansi.StringWidth(row), "row %q must cover the viewport width", row)
	for index, cell := range luaCells(t, row, width) {
		require.Truef(t, luaSameColor(cell.Style.Fg, foreground),
			"cell %d of %q must carry the page foreground", index, row)
		require.Truef(t, luaSameColor(cell.Style.Bg, background),
			"cell %d of %q must carry the page background", index, row)
	}
}

func TestLuaViewportPageResolvesDefaultCells(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		view = assert(tty.viewport({width = 6, height = 3,
			page = {foreground = "`+luaPageForeground+`", background = "`+luaPageBackground+`"}}))
		grant = assert(view:grant())
	`))
	present(t, service, luaString(t, l, "grant"), []string{"ab", "\x1b[0m  "})
	require.NoError(t, l.DoString(`
		local snapshot = view:snapshot()
		assert(#snapshot.rows == 3)
		row1, row2, row3 = snapshot.rows[1], snapshot.rows[2], snapshot.rows[3]
	`))

	foreground, background := luaPageColors(t)
	for _, name := range []string{"row1", "row2", "row3"} {
		requireLuaPageRow(t, luaString(t, l, name), 6, foreground, background)
	}
	require.Equal(t, "ab    ", ansi.Strip(luaString(t, l, "row1")))
}

func TestLuaViewportPagePreservesProducerColors(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		view = assert(tty.viewport({width = 4, height = 1,
			page = {foreground = "`+luaPageForeground+`", background = "`+luaPageBackground+`"}}))
		grant = assert(view:grant())
	`))
	present(t, service, luaString(t, l, "grant"), []string{"\x1b[38;2;255;0;0mR\x1b[0mx"})
	require.NoError(t, l.DoString(`row1 = view:snapshot().rows[1]`))

	foreground, background := luaPageColors(t)
	line := luaCells(t, luaString(t, l, "row1"), 4)
	require.Len(t, line, 4)
	require.True(t, luaSameColor(line[0].Style.Fg, color.RGBA{R: 255, A: 255}))
	require.True(t, luaSameColor(line[0].Style.Bg, background))
	for index := 1; index < len(line); index++ {
		require.True(t, luaSameColor(line[index].Style.Fg, foreground))
		require.True(t, luaSameColor(line[index].Style.Bg, background))
	}
}

func TestLuaViewportWithoutPageIsUnchanged(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		view = assert(tty.viewport({width = 6, height = 3}))
		grant = assert(view:grant())
	`))
	present(t, service, luaString(t, l, "grant"), []string{"ab", "\x1b[0m  "})
	require.NoError(t, l.DoString(`
		local snapshot = view:snapshot()
		assert(#snapshot.rows == 2, "a viewport without a page publishes producer rows verbatim")
		row1, row2 = snapshot.rows[1], snapshot.rows[2]
	`))
	require.Equal(t, "ab", luaString(t, l, "row1"))
	require.Equal(t, "\x1b[0m  ", luaString(t, l, "row2"))
}

func TestLuaViewportSetPageBumpsRevision(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		view = assert(tty.viewport({width = 4, height = 1}))
		grant = assert(view:grant())
	`))
	present(t, service, luaString(t, l, "grant"), []string{"ab"})
	require.NoError(t, l.DoString(`
		local before = view:snapshot()
		assert(view:snapshot(before.revision) == nil)
		assert(view:set_page({foreground = "`+luaPageForeground+`", background = "`+luaPageBackground+`"}))
		local after = view:snapshot(before.revision)
		assert(after ~= nil and after.revision == before.revision + 1,
			"a page change must invalidate the snapshot")
		row1 = after.rows[1]
		assert(view:set_page(nil))
		local cleared = view:snapshot()
		assert(cleared.revision == after.revision + 1)
		cleared_row1 = cleared.rows[1]
	`))
	foreground, background := luaPageColors(t)
	requireLuaPageRow(t, luaString(t, l, "row1"), 4, foreground, background)
	require.Equal(t, "ab", luaString(t, l, "cleared_row1"))
}

func TestLuaViewportRejectsInvalidPage(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		local half, half_err = tty.viewport({width = 4, height = 1, page = {foreground = "`+luaPageForeground+`"}})
		assert(half == nil and half_err)
		local malformed, malformed_err = tty.viewport({width = 4, height = 1,
			page = {foreground = "red", background = "`+luaPageBackground+`"}})
		assert(malformed == nil and malformed_err)
		local wrong_type, wrong_type_err = tty.viewport({width = 4, height = 1, page = "dark"})
		assert(wrong_type == nil and wrong_type_err)
		local empty, empty_err = tty.viewport({width = 4, height = 1, page = {}})
		assert(empty == nil and empty_err)

		local view = assert(tty.viewport({width = 4, height = 1}))
		local set, set_err = view:set_page({background = "`+luaPageBackground+`"})
		assert(set == nil and set_err)
		set, set_err = view:set_page({foreground = "#12345", background = "`+luaPageBackground+`"})
		assert(set == nil and set_err)
	`))
}

func TestLuaAttachedViewerSeesResolvedRows(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	l := luaViewportState(t, service)

	require.NoError(t, l.DoString(`
		view = assert(tty.viewport({width = 4, height = 1,
			page = {foreground = "`+luaPageForeground+`", background = "`+luaPageBackground+`"}}))
		grant = assert(view:grant())
	`))
	present(t, service, luaString(t, l, "grant"), []string{"ab"})
	require.NoError(t, l.DoString(`
		local viewer = assert(tty.attach(view:handle()))
		attached_row1 = viewer:snapshot().rows[1]
		owner_row1 = view:snapshot().rows[1]
		assert(attached_row1 == owner_row1)
	`))
	foreground, background := luaPageColors(t)
	requireLuaPageRow(t, luaString(t, l, "attached_row1"), 4, foreground, background)
}
