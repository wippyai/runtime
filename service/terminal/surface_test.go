// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/vt"
)

// screenText returns the visible rows of term as plain text.
func screenText(term *vt.Terminal) string {
	_, height := term.Screen().Size()
	rows := make([]string, height)
	for y := range rows {
		var row strings.Builder
		for _, cell := range term.Screen().Line(y) {
			if cell.Cluster == "" {
				row.WriteByte(' ')
				continue
			}
			row.WriteString(cell.Cluster)
		}
		rows[y] = row.String()
	}
	return strings.Join(rows, "\n")
}

func TestSurfaceShrinkPreservesBottomRow(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		screen := vt.New(vt.Options{Cols: 8, Rows: 4})
		surface := NewSurface(screen, ttyapi.SurfaceOptions{})
		_, err := surface.Present(ttyapi.Frame{Rows: []string{"content", "taskbar", "old", "tail"}})
		require.NoError(t, err)
		screen.Resize(8, 2)
		if invalidate {
			surface.Invalidate()
		}
		_, err = surface.Present(ttyapi.Frame{Rows: []string{"content", "taskbar"}})
		require.NoError(t, err)
		require.Contains(t, screenText(screen), "taskbar")
	}
}

func TestSurfacePreservesFullWidthRows(t *testing.T) {
	screen := vt.New(vt.Options{Cols: 8, Rows: 2})
	surface := NewSurface(screen, ttyapi.SurfaceOptions{})
	for _, rows := range [][]string{
		{"12345678", "abcdefgh"},
		{"87654321", "hgfedcba"},
	} {
		_, err := surface.Present(ttyapi.Frame{Rows: rows})
		require.NoError(t, err)
		for _, row := range rows {
			require.Contains(t, screenText(screen), row)
		}
	}
}

type failingWriter struct {
	err     error
	succeed int
	writes  int
}

func (w *failingWriter) Write(payload []byte) (int, error) {
	w.writes++
	if w.writes <= w.succeed {
		return len(payload), nil
	}
	return 0, w.err
}

func TestSurfaceCloseIsTerminalEvenWhenRestoreFails(t *testing.T) {
	writeErr := errors.New("output unavailable")
	writer := &failingWriter{err: writeErr, succeed: 1}
	surface := NewSurface(writer, ttyapi.SurfaceOptions{})
	_, err := surface.Present(ttyapi.Frame{Rows: []string{"open"}})
	require.NoError(t, err)

	require.ErrorIs(t, surface.Close(), writeErr)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"late frame"}})
	require.ErrorContains(t, err, "surface is closed")
	require.ErrorIs(t, surface.Close(), writeErr)
}

func TestSurfaceInvalidateRetainsExtentAndForcesCompleteCommit(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output, ttyapi.SurfaceOptions{})
	_, err := surface.Present(ttyapi.Frame{Rows: []string{"one", "two", "three"}})
	require.NoError(t, err)
	output.Reset()

	surface.Invalidate()
	stats, err := surface.Present(ttyapi.Frame{Rows: []string{"one"}})
	require.NoError(t, err)
	require.Equal(t, 3, stats.ChangedRows)
	require.Contains(t, output.String(), "\x1b[2;1H\x1b[0m\x1b[K")
	require.Contains(t, output.String(), "\x1b[3;1H\x1b[0m\x1b[K")
}

func TestSurfaceInvalidateCommitsEmptyFrame(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output, ttyapi.SurfaceOptions{})
	surface.Invalidate()
	stats, err := surface.Present(ttyapi.Frame{})
	require.NoError(t, err)
	require.NotZero(t, stats.Bytes)
	require.NotEmpty(t, output.String())
}

func TestSurfaceInvalidateRestoresCursorForEmptyFrame(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output, ttyapi.SurfaceOptions{})
	_, err := surface.Present(ttyapi.Frame{Cursor: &ttyapi.Cursor{
		Column: 4, Row: 2, Visible: false,
	}})
	require.NoError(t, err)
	output.Reset()

	surface.Invalidate()
	_, err = surface.Present(ttyapi.Frame{})
	require.NoError(t, err)
	require.Contains(t, output.String(), "\x1b[3;5H")
	require.Contains(t, output.String(), "\x1b[?25l")
}

func TestSurfaceCloseBeforePresentDoesNotMutateTerminal(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output, ttyapi.SurfaceOptions{
		AlternateScreen: true, HideCursor: true, Synchronized: true,
	})
	require.NoError(t, surface.Close())
	require.Empty(t, output.String())
}
