// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestKittyFunctionalKeysRoundTrip(t *testing.T) {
	keys := map[string]rune{
		"left": uv.KeyLeft, "right": uv.KeyRight, "up": uv.KeyUp, "down": uv.KeyDown,
		"home": uv.KeyHome, "end": uv.KeyEnd, "insert": uv.KeyInsert, "delete": uv.KeyDelete,
		"pgup": uv.KeyPgUp, "pgdown": uv.KeyPgDown,
		"f1": uv.KeyF1, "f2": uv.KeyF2, "f3": uv.KeyF3, "f4": uv.KeyF4,
		"f5": uv.KeyF5, "f6": uv.KeyF6, "f7": uv.KeyF7, "f8": uv.KeyF8,
		"f9": uv.KeyF9, "f10": uv.KeyF10, "f11": uv.KeyF11, "f12": uv.KeyF12,
	}
	for name, code := range keys {
		for flags := 1; flags <= ansi.KittyAllFlags; flags++ {
			for mods := 0; mods < 8; mods++ {
				for _, action := range []string{"press", "release"} {
					t.Run(fmt.Sprintf("%s/%d/%d/%s", name, flags, mods, action), func(t *testing.T) {
						event := ttyapi.Event{Type: "key", KeyType: name, Key: name, Action: action,
							Shift: mods&1 != 0, Alt: mods&2 != 0, Ctrl: mods&4 != 0}
						seq := encodeKittyKey(event, flags)
						if action == "release" && flags&ansi.KittyReportEventTypes == 0 {
							require.Empty(t, seq)
							return
						}
						var decoder uv.EventDecoder
						n, decoded := decoder.Decode([]byte(seq))
						require.Equal(t, len(seq), n)
						key, ok := decoded.(uv.KeyEvent)
						require.True(t, ok, "%q decoded as %T", seq, decoded)
						require.Equal(t, code, key.Key().Code)
						var expectedMod uv.KeyMod
						if event.Shift {
							expectedMod |= uv.ModShift
						}
						if event.Alt {
							expectedMod |= uv.ModAlt
						}
						if event.Ctrl {
							expectedMod |= uv.ModCtrl
						}
						require.Equal(t, expectedMod, key.Key().Mod)
						// The pinned decoder drops release types for tilde keys;
						// check their event-type parameter on the wire below.
						if action == "release" && seq[len(seq)-1] != '~' {
							require.IsType(t, uv.KeyReleaseEvent{}, decoded)
						}
						if action == "release" {
							require.Contains(t, seq, ":3")
						}
					})
				}
			}
		}
	}
}

func TestKittyControlCompatibility(t *testing.T) {
	for _, name := range []string{"enter", "tab", "backspace"} {
		for _, flags := range []int{1, 3, 7} {
			event := ttyapi.Event{Type: "key", KeyType: name, Key: name, Action: "press"}
			require.Equal(t, fixedKeys[name], encodeKittyKey(event, flags))
			event.Action = "release"
			require.Empty(t, encodeKittyKey(event, flags))
		}
	}
}

func TestKittyKeyboardNegotiationEncodesArrowsAndRelease(t *testing.T) {
	var state keyboardState
	state.push(ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportEventTypes)
	press, handled := state.encode(ttyapi.Event{Type: "key", KeyType: "down", Action: "press"})
	require.True(t, handled)
	require.Equal(t, "\x1b[1;1:1B", press)
	release, handled := state.encode(ttyapi.Event{Type: "key", KeyType: "down", Action: "release"})
	require.True(t, handled)
	require.Equal(t, "\x1b[1;1:3B", release)
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
