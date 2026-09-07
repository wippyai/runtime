// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func wheel(button string) ttyapi.Event {
	return ttyapi.Event{Type: "mouse", Action: "wheel", Button: button, X: 3, Y: 4}
}

func newTestInputState() *inputState {
	state := &inputState{}
	state.init()
	return state
}

func TestAlternateScrollSendsCursorKeysOnAltScreen(t *testing.T) {
	for _, mode := range []ansi.Mode{ansi.ModeAltScreen, ansi.ModeAltScreenSaveCursor} {
		state := newTestInputState()
		state.enable(mode)
		require.Equal(t, "\x1b[A\x1b[A\x1b[A", state.mouse(wheel("wheel_up")))
		require.Equal(t, "\x1b[B\x1b[B\x1b[B", state.mouse(wheel("wheel_down")))
	}
}

func TestAlternateScrollUsesApplicationCursorKeys(t *testing.T) {
	state := newTestInputState()
	state.enable(ansi.ModeAltScreenSaveCursor)
	state.enable(ansi.ModeCursorKeys)
	require.Equal(t, "\x1bOA\x1bOA\x1bOA", state.mouse(wheel("wheel_up")))
	require.Equal(t, "\x1bOB\x1bOB\x1bOB", state.mouse(wheel("wheel_down")))
}

func TestAlternateScrollIgnoresWheelOnMainScreen(t *testing.T) {
	state := newTestInputState()
	require.Empty(t, state.mouse(wheel("wheel_up")))
	require.Empty(t, state.mouse(wheel("wheel_down")))
}

func TestAlternateScrollDisabledIgnoresWheel(t *testing.T) {
	state := newTestInputState()
	state.enable(ansi.ModeAltScreenSaveCursor)
	state.disable(modeAlternateScroll)
	require.Empty(t, state.mouse(wheel("wheel_up")))
	state.enable(modeAlternateScroll)
	require.Equal(t, "\x1b[A\x1b[A\x1b[A", state.mouse(wheel("wheel_up")))
}

func TestAlternateScrollYieldsToMouseTracking(t *testing.T) {
	state := newTestInputState()
	state.enable(ansi.ModeAltScreenSaveCursor)
	state.enable(ansi.ModeMouseNormal)
	require.Equal(t, ansi.MouseX10(ansi.EncodeMouseButton(ansi.MouseWheelUp, false, false, false, false), 2, 3),
		state.mouse(wheel("wheel_up")))
	state.enable(ansi.ModeMouseExtSgr)
	require.Equal(t, ansi.MouseSgr(ansi.EncodeMouseButton(ansi.MouseWheelDown, false, false, false, false), 2, 3, false),
		state.mouse(wheel("wheel_down")))
}

func TestAlternateScrollStopsAfterLeavingAltScreen(t *testing.T) {
	state := newTestInputState()
	state.enable(ansi.ModeAltScreenSaveCursor)
	require.Equal(t, "\x1b[A\x1b[A\x1b[A", state.mouse(wheel("wheel_up")))
	state.disable(ansi.ModeAltScreenSaveCursor)
	require.Empty(t, state.mouse(wheel("wheel_up")))
}

func TestProxyTranslatesWheelUnderAlternateScroll(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	_, err = proxy.screen.Write([]byte(ansi.SetModeAltScreenSaveCursor))
	require.NoError(t, err)
	require.NoError(t, proxy.handle(wheel("wheel_down")))
	require.Equal(t, "\x1b[B\x1b[B\x1b[B", string(<-process.input))
}
