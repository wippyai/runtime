// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestKittyKeyboardNegotiationEncodesArrowsAndRelease(t *testing.T) {
	var state keyboardState
	state.push(ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportEventTypes)
	press, handled := state.encode(ttyapi.Event{Type: "key", KeyType: "down", Action: "press"})
	require.True(t, handled)
	require.Equal(t, "\x1b[57353;1:1u", press)
	release, handled := state.encode(ttyapi.Event{Type: "key", KeyType: "down", Action: "release"})
	require.True(t, handled)
	require.Equal(t, "\x1b[57353;1:3u", release)
	state.pop(1)
	_, handled = state.encode(ttyapi.Event{Type: "key", KeyType: "down", Action: "press"})
	require.False(t, handled)
}

func TestModifyOtherKeysEncodesModifiedRunes(t *testing.T) {
	var state keyboardState
	state.setModifyOtherKeys(2)
	sequence, handled := state.encode(ttyapi.Event{Type: "key", KeyType: "runes", Key: "x", Ctrl: true, Action: "press"})
	require.True(t, handled)
	require.Equal(t, "\x1b[27;5;120~", sequence)
}
