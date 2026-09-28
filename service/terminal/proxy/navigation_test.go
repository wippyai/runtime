// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestLegacyNavigationMatrix(t *testing.T) {
	for name, code := range map[string]string{
		"up": "A", "down": "B", "right": "C", "left": "D", "home": "H", "end": "F",
		"insert": "2~", "delete": "3~", "pgup": "5~", "pgdown": "6~",
		"f1": "P", "f2": "Q", "f3": "R", "f4": "S", "f5": "15~", "f6": "17~",
		"f7": "18~", "f8": "19~", "f9": "20~", "f10": "21~", "f11": "23~", "f12": "24~",
	} {
		for mask := 0; mask < 8; mask++ {
			for _, application := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/mod%d/app%t", name, mask, application), func(t *testing.T) {
					var input inputState
					input.appCursor.Store(application)
					event := ttyapi.Event{Type: "key", KeyType: name, Action: "press", Shift: mask&1 != 0, Alt: mask&2 != 0, Ctrl: mask&4 != 0}
					want := "\x1b[" + code
					if mask != 0 {
						if code[len(code)-1] == '~' {
							want = fmt.Sprintf("\x1b[%s;%d~", code[:len(code)-1], mask+1)
						} else {
							want = fmt.Sprintf("\x1b[1;%d%s", mask+1, code)
						}
					} else if (application && len(code) == 1) || (name == "f1" || name == "f2" || name == "f3" || name == "f4") {
						want = "\x1bO" + code
					}
					require.Equal(t, want, input.key(event))
					event.Action = "release"
					require.Empty(t, input.key(event))
				})
			}
		}
	}
}

func TestLegacyNavigationModifiers(t *testing.T) {
	for _, item := range []struct {
		name, want       string
		shift, alt, ctrl bool
	}{
		{"home", "\x1b[1;5H", false, false, true},
		{"end", "\x1b[1;2F", true, false, false},
		{"delete", "\x1b[3;5~", false, false, true},
		{"insert", "\x1b[2;2~", true, false, false},
		{"pgup", "\x1b[5;3~", false, true, false},
		{"pgdown", "\x1b[6;6~", true, false, true},
		{"tab", "\x1b[Z", true, false, false},
		{"f5", "\x1b[15;5~", false, false, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			var input inputState
			event := ttyapi.Event{Type: "key", KeyType: item.name, Action: "press", Shift: item.shift, Alt: item.alt, Ctrl: item.ctrl}
			require.Equal(t, item.want, input.key(event))
			event.Action = "release"
			require.Empty(t, input.key(event))
		})
	}
}
