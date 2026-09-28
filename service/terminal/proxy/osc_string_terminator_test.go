// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// oscTitleChild returns a proxy whose child emits the given bytes once.
func oscTitleChild(t *testing.T, width, height int, output string) (*Proxy, *testSurface) {
	t.Helper()
	process := &testProcess{
		stdout: io.NopCloser(strings.NewReader("")),
		input:  make(chan []byte, 1),
	}
	surface := &testSurface{}
	proxy, err := New(process, surface, width, height)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte(output))
	require.NoError(t, err)
	require.NoError(t, proxy.present())
	return proxy, surface
}

func presentedRow(surface *testSurface, row int) string {
	surface.mu.Lock()
	defer surface.mu.Unlock()
	if row >= len(surface.rows) {
		return ""
	}
	return strings.TrimRight(surface.rows[row], " ")
}

// A 0x9C byte inside a multi-byte rune is a UTF-8 continuation byte, not the
// 8-bit String Terminator. Treating it as ST truncates the OSC payload mid
// rune and prints the remaining title bytes to the screen, which leaves stale
// cells an application never asked to draw. Claude Code sets such a title
// (U+2733) on every status update.
func TestProxyOSCTitleWithMultibyteRuneDoesNotLeakIntoGrid(t *testing.T) {
	// U+2733 is E2 9C B3: the middle byte is 0x9C.
	_, surface := oscTitleChild(t, 20, 2, "\x1b]0;\u2733 Pong reply\x07xy")
	require.Equal(t, "xy", presentedRow(surface, 0))
}

func TestProxyOSCTitleWithFourByteRuneDoesNotLeakIntoGrid(t *testing.T) {
	// U+1F600 is F0 9F 98 80; no continuation byte is 0x9C, so this guards the
	// general 0x80-0xBF continuation handling rather than only the ST byte.
	_, surface := oscTitleChild(t, 20, 2, "\x1b]0;\U0001F600 marker\x07xy")
	require.Equal(t, "xy", presentedRow(surface, 0))
}

// A standalone 0x9C that does not continue a rune is still the 8-bit ST and
// must terminate the string. The fix must not disable that.
func TestProxyEightBitStringTerminatorStillTerminatesOSC(t *testing.T) {
	_, surface := oscTitleChild(t, 20, 2, "\x1b]0;hi\x9cZ")
	require.Equal(t, "Z", presentedRow(surface, 0))
}

// ESC-still terminates an OSC string carrying a multi-byte rune.
func TestProxyEscStringTerminatorWithMultibyteRune(t *testing.T) {
	_, surface := oscTitleChild(t, 20, 2, "\x1b]0;\u2733 Pong reply\x1b\\xy")
	require.Equal(t, "xy", presentedRow(surface, 0))
}
