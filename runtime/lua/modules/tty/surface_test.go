// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
	terminalsvc "github.com/wippyai/runtime/service/terminal"
)

func testPhysicalSurface(out io.Writer, opts ttyapi.SurfaceOptions) *surfaceWrapper {
	return &surfaceWrapper{backend: terminalsvc.NewSurface(out, opts)}
}

func TestSurfacePresentDiffsRowsAndSkipsUnchangedFrame(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{})

	changed, written, err := surface.present([]string{"one", "two", "three"})
	require.NoError(t, err)
	assert.Equal(t, 3, changed)
	assert.Equal(t, output.Len(), written)
	assert.Contains(t, output.String(), "\x1b[1;1H\x1b[0m\x1b[Kone")
	assert.Contains(t, output.String(), "\x1b[3;1H\x1b[0m\x1b[Kthree")

	output.Reset()
	changed, written, err = surface.present([]string{"one", "two", "three"})
	require.NoError(t, err)
	assert.Zero(t, changed)
	assert.Zero(t, written)
	assert.Zero(t, output.Len())

	changed, written, err = surface.present([]string{"one", "changed", "three"})
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	assert.Equal(t, output.Len(), written)
	assert.NotContains(t, output.String(), "\x1b[1;1H")
	assert.Contains(t, output.String(), "\x1b[2;1H\x1b[0m\x1b[Kchanged")
}

func TestSurfacePresentClearsRemovedRows(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{})
	require.NoError(t, func() error {
		_, _, err := surface.present([]string{"one", "two", "three"})
		return err
	}())
	output.Reset()

	changed, _, err := surface.present([]string{"one"})
	require.NoError(t, err)
	assert.Equal(t, 2, changed)
	assert.Contains(t, output.String(), "\x1b[2;1H\x1b[0m\x1b[K")
	assert.Contains(t, output.String(), "\x1b[3;1H\x1b[0m\x1b[K")
}

func TestSurfacePresentsCursorWithoutRepaintingRows(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{HideCursor: true})
	frame := ttyapi.Frame{Rows: []string{"one", "two"}, Cursor: &ttyapi.Cursor{
		Column: 2, Row: 1, Visible: true,
	}}
	changed, _, err := surface.presentFrame(frame)
	require.NoError(t, err)
	assert.Equal(t, 2, changed)
	assert.Contains(t, output.String(), "\x1b[2;3H\x1b[?25h")

	output.Reset()
	changed, written, err := surface.presentFrame(frame)
	require.NoError(t, err)
	assert.Zero(t, changed)
	assert.Zero(t, written)

	frame.Cursor = &ttyapi.Cursor{Column: 4, Row: 0, Visible: false}
	changed, _, err = surface.presentFrame(frame)
	require.NoError(t, err)
	assert.Zero(t, changed)
	assert.Equal(t, "\x1b[1;5H\x1b[?25l", output.String())
}

func TestSurfaceRestoresUnchangedCursorAfterRowDamage(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{HideCursor: true})
	frame := ttyapi.Frame{Rows: []string{"one", "two"}, Cursor: &ttyapi.Cursor{
		Column: 2, Row: 0, Visible: true,
	}}
	_, _, err := surface.presentFrame(frame)
	require.NoError(t, err)

	output.Reset()
	frame.Rows[1] = "a changed row reaching the right edge"
	changed, _, err := surface.presentFrame(frame)
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	assert.Contains(t, output.String(), "\x1b[2;1H")
	assert.True(t, strings.HasSuffix(output.String(), "\x1b[1;3H\x1b[?25h"),
		"row damage must restore the logical cursor as the final frame operation")
}

func TestSurfaceSynchronizedOutputWrapsChangedFrames(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{Synchronized: true})

	changed, written, err := surface.present([]string{"one", "two"})
	require.NoError(t, err)
	assert.Equal(t, 2, changed)
	assert.Equal(t, output.Len(), written)
	assert.True(t, strings.HasPrefix(output.String(), "\x1b[?2026h"))
	assert.True(t, strings.HasSuffix(output.String(), "\x1b[?2026l"))

	output.Reset()
	changed, written, err = surface.present([]string{"one", "two"})
	require.NoError(t, err)
	assert.Zero(t, changed)
	assert.Zero(t, written)
	assert.Empty(t, output.String(), "unchanged frames must not toggle sync mode")

	changed, written, err = surface.present([]string{"changed", "two"})
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	assert.Equal(t, output.Len(), written)
	assert.Equal(t, 1, strings.Count(output.String(), "\x1b[?2026h"))
	assert.Equal(t, 1, strings.Count(output.String(), "\x1b[?2026l"))
}

func TestSurfaceSynchronizedLifecycleRestoresMode(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{
		AlternateScreen: true, HideCursor: true, Synchronized: true,
	})

	_, _, err := surface.present([]string{"frame"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(output.String(),
		"\x1b[?2026h\x1b[?1049h\x1b[?25l"))
	assert.True(t, strings.HasSuffix(output.String(), "\x1b[?2026l"))

	output.Reset()
	require.NoError(t, surface.close())
	assert.Equal(t, "\x1b[?2026l\x1b[0m\x1b[?25h\x1b[?1049l", output.String())
}

func TestSurfaceLifecycleIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{
		AlternateScreen: true, HideCursor: true,
	})
	_, _, err := surface.present([]string{"frame"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(output.String(), "\x1b[?1049h\x1b[?25l"))
	require.NoError(t, surface.close())
	sizeAfterClose := output.Len()
	require.NoError(t, surface.close())
	assert.Equal(t, sizeAfterClose, output.Len())
	assert.True(t, strings.HasSuffix(output.String(), "\x1b[0m\x1b[?25h\x1b[?1049l"))
	_, _, err = surface.present([]string{"late"})
	assert.ErrorContains(t, err, "closed")
}

type shortSurfaceWriter struct{}

func (shortSurfaceWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}

type recoverableSurfaceWriter struct {
	bytes.Buffer
	fail bool
}

func (w *recoverableSurfaceWriter) Write(payload []byte) (int, error) {
	if w.fail {
		return 0, errors.New("write failed")
	}
	return w.Buffer.Write(payload)
}

func TestSurfaceDoesNotCommitFailedOutput(t *testing.T) {
	writer := &recoverableSurfaceWriter{fail: true}
	surface := testPhysicalSurface(writer, ttyapi.SurfaceOptions{})
	_, _, err := surface.present([]string{"not committed"})
	assert.ErrorContains(t, err, "write failed")
	writer.fail = false
	changed, _, err := surface.present([]string{"not committed"})
	require.NoError(t, err)
	assert.Equal(t, 1, changed, "a failed frame must not become the diff baseline")
}

func TestSurfaceDoesNotCommitShortWrite(t *testing.T) {
	surface := testPhysicalSurface(shortSurfaceWriter{}, ttyapi.SurfaceOptions{})
	_, _, err := surface.present([]string{"not committed"})
	assert.ErrorIs(t, err, io.ErrShortWrite)
}

func TestSurfaceCloseReturnsFirstRestoreFailure(t *testing.T) {
	writer := &recoverableSurfaceWriter{fail: true}
	surface := testPhysicalSurface(writer, ttyapi.SurfaceOptions{HideCursor: true})
	surface.present([]string{"open"})
	assert.ErrorContains(t, surface.close(), "write failed")
	writer.fail = false
	assert.ErrorContains(t, surface.close(), "write failed")
	assert.Empty(t, writer.String(), "a closed surface must not retry terminal mutations")
}

func TestPhysicalRowOnlyFrameRestoresLastExplicitCursor(t *testing.T) {
	var output bytes.Buffer
	surface := testPhysicalSurface(&output, ttyapi.SurfaceOptions{})
	_, _, err := surface.presentFrame(ttyapi.Frame{Rows: []string{"first"},
		Cursor: &ttyapi.Cursor{Column: 2, Row: 0, Visible: true}})
	require.NoError(t, err)
	output.Reset()
	_, _, err = surface.present([]string{"changed"})
	require.NoError(t, err)
	assert.Contains(t, output.String(), "\x1b[1;3H\x1b[?25h")
}

func TestTTYSurfaceLuaAPI(t *testing.T) {
	var output bytes.Buffer
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	ctx := ctxapi.NewRootContext()
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	defer frame.Close()
	tc := terminalapi.NewTerminalContext(nil, &output, nil)
	tc.Surface = func(options ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
		return terminalsvc.NewSurface(&output, options), nil
	}
	require.NoError(t, terminalapi.WithTerminalContext(ctx, tc))
	l.SetContext(ctx)

	err := l.DoString(`
		local surface, open_err = tty.surface({alternate_screen = true})
		if not surface then error(tostring(open_err)) end
		local first, first_err = surface:present({"one", "two"})
		if first_err or first.changed_rows ~= 2 then error("first frame") end
		local same, same_err = surface:present({"one", "two"})
		if same_err or same.changed_rows ~= 0 or same.bytes_written ~= 0 then
			error("unchanged frame")
		end
		local cursor, cursor_err = surface:present({"one", "two"}, {
			cursor = {x = 3, y = 2, visible = true},
		})
		if cursor_err or cursor.changed_rows ~= 0 then error("cursor frame") end
		local ok, close_err = surface:close()
		if not ok then error(tostring(close_err)) end
	`)
	require.NoError(t, err)
}

func BenchmarkSurfacePresentUnchanged200Rows(b *testing.B) {
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = strings.Repeat("x", 120)
	}
	surface := testPhysicalSurface(io.Discard, ttyapi.SurfaceOptions{})
	surface.present(rows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		surface.present(rows)
	}
}

func BenchmarkSurfacePresentOneChangedRow(b *testing.B) {
	rowsA := make([]string, 200)
	rowsB := make([]string, 200)
	for i := range rowsA {
		rowsA[i] = strings.Repeat("x", 120)
		rowsB[i] = rowsA[i]
	}
	rowsB[100] = "y" + rowsB[100][1:]
	surface := testPhysicalSurface(io.Discard, ttyapi.SurfaceOptions{})
	surface.present(rowsA)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := rowsA
		if i%2 == 0 {
			rows = rowsB
		}
		surface.present(rows)
	}
}

func BenchmarkSurfacePresentOneChangedRow1000Rows(b *testing.B) {
	rowsA := make([]string, 1000)
	rowsB := make([]string, 1000)
	for i := range rowsA {
		rowsA[i] = strings.Repeat("x", 120)
		rowsB[i] = rowsA[i]
	}
	rowsB[500] = "y" + rowsB[500][1:]
	surface := testPhysicalSurface(io.Discard, ttyapi.SurfaceOptions{})
	surface.present(rowsA)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := rowsA
		if i%2 == 0 {
			rows = rowsB
		}
		surface.present(rows)
	}
}
