// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestSurfaceCloseIsTerminalEvenWhenRestoreFails(t *testing.T) {
	writeErr := errors.New("output unavailable")
	surface := NewSurface(failingWriter{err: writeErr}, ttyapi.SurfaceOptions{})

	require.ErrorIs(t, surface.Close(), writeErr)
	_, err := surface.Present(ttyapi.Frame{Rows: []string{"late frame"}})
	require.ErrorContains(t, err, "surface is closed")
	require.ErrorIs(t, surface.Close(), writeErr)
}
