// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"unicode"
	"unicode/utf8"

	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/input"
)

func modifiers(event ttyapi.Event) input.Mod {
	var mod input.Mod
	if event.Shift {
		mod |= input.ModShift
	}
	if event.Alt {
		mod |= input.ModAlt
	}
	if event.Ctrl {
		mod |= input.ModCtrl
	}
	return mod
}

// keyEvent converts a public key event into the emulator's input event. It
// reports false for keys the emulator cannot name.
func keyEvent(event ttyapi.Event) (input.Event, bool) {
	key := input.Key{Mod: modifiers(event)}
	name := event.KeyType
	if name == "" {
		name = event.Key
	}
	if name != "runes" {
		if code, ok := input.KeyCode(name); ok {
			key.Code = code
			return keyAction(event, key), true
		}
		if event.KeyType != "" {
			return nil, false
		}
	}
	r, _ := utf8.DecodeRuneInString(event.Key)
	if r == utf8.RuneError {
		return nil, false
	}
	key.Code = unicode.ToLower(r)
	if event.Shift {
		key.ShiftedCode = unicode.ToUpper(r)
	}
	if !event.Ctrl && !event.Alt {
		key.Text = event.Key
	}
	return keyAction(event, key), true
}

func keyAction(event ttyapi.Event, key input.Key) input.Event {
	if event.Action == "release" {
		return input.KeyReleaseEvent(key)
	}
	return input.KeyPressEvent(key)
}

var mouseButtons = map[string]input.MouseButton{
	"none": input.MouseNone, "left": input.MouseLeft, "middle": input.MouseMiddle,
	"right": input.MouseRight, "wheel_up": input.MouseWheelUp, "wheel_down": input.MouseWheelDown,
}

// mouseEvent converts a public mouse event, whose coordinates are one-based,
// into the emulator's zero-based input event.
func mouseEvent(event ttyapi.Event) (input.Event, bool) {
	button, ok := mouseButtons[event.Button]
	if !ok {
		return nil, false
	}
	mouse := input.Mouse{X: event.X - 1, Y: event.Y - 1, Button: button, Mod: modifiers(event)}
	if button == input.MouseWheelUp || button == input.MouseWheelDown {
		return input.MouseWheelEvent(mouse), true
	}
	switch event.Action {
	case "press":
		return input.MouseClickEvent(mouse), true
	case "release":
		return input.MouseReleaseEvent(mouse), true
	case "motion":
		return input.MouseMotionEvent(mouse), true
	}
	return nil, false
}
