// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// decodeCSI parses a complete CSI key report: number, optional modifier and
// event-type parameters, and a final byte.
func decodeCSI(t *testing.T, seq string) (number, mods, eventType int, final byte) {
	t.Helper()
	require.True(t, strings.HasPrefix(seq, "\x1b["), "%q", seq)
	body := seq[2 : len(seq)-1]
	final = seq[len(seq)-1]
	number, mods, eventType = 1, 1, 1
	if body == "" {
		return
	}
	fields := strings.Split(body, ";")
	var err error
	if fields[0] != "" {
		number, err = strconv.Atoi(fields[0])
		require.NoError(t, err, "%q", seq)
	}
	if len(fields) > 1 {
		parts := strings.Split(fields[1], ":")
		mods, err = strconv.Atoi(parts[0])
		require.NoError(t, err, "%q", seq)
		if len(parts) > 1 {
			eventType, err = strconv.Atoi(parts[1])
			require.NoError(t, err, "%q", seq)
		}
		require.LessOrEqual(t, len(parts), 2, "%q", seq)
	}
	require.LessOrEqual(t, len(fields), 2, "%q", seq)
	return
}

func TestKittyFunctionalKeysRoundTrip(t *testing.T) {
	keys := map[string]struct {
		number int
		final  byte
	}{
		"left": {1, 'D'}, "right": {1, 'C'}, "up": {1, 'A'}, "down": {1, 'B'},
		"home": {1, 'H'}, "end": {1, 'F'}, "insert": {2, '~'}, "delete": {3, '~'},
		"pgup": {5, '~'}, "pgdown": {6, '~'},
		"f1": {1, 'P'}, "f2": {1, 'Q'}, "f3": {13, '~'}, "f4": {1, 'S'},
		"f5": {15, '~'}, "f6": {17, '~'}, "f7": {18, '~'}, "f8": {19, '~'},
		"f9": {20, '~'}, "f10": {21, '~'}, "f11": {23, '~'}, "f12": {24, '~'},
	}
	for name, want := range keys {
		for flags := 1; flags <= kittyAllFlags; flags++ {
			for mods := 0; mods < 8; mods++ {
				for _, action := range []string{"press", "release"} {
					t.Run(fmt.Sprintf("%s/%d/%d/%s", name, flags, mods, action), func(t *testing.T) {
						event := ttyapi.Event{Type: "key", KeyType: name, Key: name, Action: action,
							Shift: mods&1 != 0, Alt: mods&2 != 0, Ctrl: mods&4 != 0}
						seq := encodeKittyKey(event, flags)
						if action == "release" && flags&kittyReportEventTypes == 0 {
							require.Empty(t, seq)
							return
						}
						number, gotMods, eventType, final := decodeCSI(t, seq)
						require.Equal(t, want.number, number, "%q", seq)
						require.Equal(t, want.final, final, "%q", seq)
						require.Equal(t, 1+mods, gotMods, "%q", seq)
						if action == "release" {
							require.Equal(t, 3, eventType, "%q", seq)
						} else {
							require.Equal(t, 1, eventType, "%q", seq)
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
	state.push(kittyDisambiguateEscapeCodes | kittyReportEventTypes)
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

func TestKittyKeyboardNegotiationThroughEmulator(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, "\x1b[>3u\x1b[?u")
	go func() { _ = proxy.copyResponsesOnce() }()
	require.Equal(t, "\x1b[?3u", string(<-process.input))

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "down", Action: "press"}))
	require.Equal(t, "\x1b[1;1:1B", string(<-process.input))

	proxy.feed(t, "\x1b[<u\x1b[?u")
	go func() { _ = proxy.copyResponsesOnce() }()
	require.Equal(t, "\x1b[?0u", string(<-process.input))
}

func TestModifyOtherKeysNegotiationThroughEmulator(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, "\x1b[>4;2m")
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "runes", Key: "x", Ctrl: true, Action: "press"}))
	require.Equal(t, "\x1b[27;5;120~", string(<-process.input))
}

func (p *Proxy) copyResponsesOnce() error {
	data, ok := p.responses.next()
	if !ok {
		return nil
	}
	return p.writeBytes(data)
}
