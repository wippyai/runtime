// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func wheel(button string) ttyapi.Event {
	return ttyapi.Event{Type: "mouse", Action: "wheel", Button: button, X: 3, Y: 4}
}

const (
	setAltScreen          = "\x1b[?1049h"
	resetAltScreen        = "\x1b[?1049l"
	setApplicationCursor  = "\x1b[?1h"
	setMouseNormal        = "\x1b[?1000h"
	setMouseSGR           = "\x1b[?1006h"
	setAlternateScroll    = "\x1b[?1007h"
	resetAlternateScroll  = "\x1b[?1007l"
	setLegacyAltScreen    = "\x1b[?47h"
	setSaveCursorAltBuf   = "\x1b[?1047h"
	resetSaveCursorAltBuf = "\x1b[?1047l"
)

// x10Mouse is the X10 wire encoding of a button code at a one-based cell.
func x10Mouse(code, column, row int) string {
	return fmt.Sprintf("\x1b[M%c%c%c", rune(32+code), rune(32+column), rune(32+row))
}

func newModeProxy(t *testing.T) (*Proxy, *testProcess) {
	t.Helper()
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	return proxy, process
}

func (p *Proxy) feed(t *testing.T, output string) {
	t.Helper()
	_, err := p.writeOutput([]byte(output))
	require.NoError(t, err)
}

func TestAlternateScrollSendsCursorKeysOnAltScreen(t *testing.T) {
	for _, mode := range []string{setAltScreen, setLegacyAltScreen, setSaveCursorAltBuf} {
		proxy, process := newModeProxy(t)
		proxy.feed(t, mode)
		require.NoError(t, proxy.handle(wheel("wheel_up")))
		require.Equal(t, "\x1b[A\x1b[A\x1b[A", string(<-process.input))
		require.NoError(t, proxy.handle(wheel("wheel_down")))
		require.Equal(t, "\x1b[B\x1b[B\x1b[B", string(<-process.input))
	}
}

func TestAlternateScrollUsesApplicationCursorKeys(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setAltScreen+setApplicationCursor)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Equal(t, "\x1bOA\x1bOA\x1bOA", string(<-process.input))
	require.NoError(t, proxy.handle(wheel("wheel_down")))
	require.Equal(t, "\x1bOB\x1bOB\x1bOB", string(<-process.input))
}

func TestAlternateScrollIgnoresWheelOnMainScreen(t *testing.T) {
	state := &inputState{}
	state.init()
	require.Empty(t, state.alternateScroll(wheel("wheel_up")))
	require.Empty(t, state.alternateScroll(wheel("wheel_down")))
}

func TestAlternateScrollDisabledIgnoresWheel(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setAltScreen+resetAlternateScroll)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	select {
	case unexpected := <-process.input:
		t.Fatalf("wheel reached child with alternate scroll disabled: %q", unexpected)
	default:
	}
	proxy.feed(t, setAlternateScroll)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Equal(t, "\x1b[A\x1b[A\x1b[A", string(<-process.input))
}

func TestAlternateScrollYieldsToMouseTracking(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setAltScreen+setMouseNormal)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Equal(t, x10Mouse(64, 3, 4), string(<-process.input))
	proxy.feed(t, setMouseSGR)
	require.NoError(t, proxy.handle(wheel("wheel_down")))
	require.Equal(t, "\x1b[<65;3;4M", string(<-process.input))
}

func TestAlternateScrollStopsAfterLeavingAltScreen(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setAltScreen)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Equal(t, "\x1b[A\x1b[A\x1b[A", string(<-process.input))
	proxy.feed(t, resetAltScreen)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	select {
	case unexpected := <-process.input:
		t.Fatalf("wheel reached child on the main screen: %q", unexpected)
	default:
	}
}

func TestProxyTranslatesWheelUnderAlternateScroll(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setAltScreen)
	require.NoError(t, proxy.handle(wheel("wheel_down")))
	require.Equal(t, "\x1b[B\x1b[B\x1b[B", string(<-process.input))
}

func TestMouseEncodingFollowsNegotiatedProtocol(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, setMouseNormal+setMouseSGR)
	press := ttyapi.Event{Type: "mouse", Action: "press", Button: "left", X: 5, Y: 6, Ctrl: true}
	require.NoError(t, proxy.handle(press))
	require.Equal(t, "\x1b[<16;5;6M", string(<-process.input))
	press.Action = "release"
	require.NoError(t, proxy.handle(press))
	require.Equal(t, "\x1b[<16;5;6m", string(<-process.input))
}
