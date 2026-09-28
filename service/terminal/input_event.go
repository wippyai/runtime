// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/input"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// TopicTTYEvents is the relay topic for terminal input events.
const TopicTTYEvents = ttyapi.TopicEvents

// TTYEvent is the abstract terminal input event sent via relay as payload.Golang.
type TTYEvent = ttyapi.Event

// keyTypeName maps key codes to string names for Lua consumption.
var keyTypeName = map[rune]string{
	input.KeyEnter:     "enter",
	input.KeyTab:       "tab",
	input.KeyBackspace: "backspace",
	input.KeyEscape:    "esc",
	input.KeySpace:     "space",
	input.KeyUp:        "up",
	input.KeyDown:      "down",
	input.KeyLeft:      "left",
	input.KeyRight:     "right",
	input.KeyHome:      "home",
	input.KeyEnd:       "end",
	input.KeyPgUp:      "pgup",
	input.KeyPgDown:    "pgdown",
	input.KeyInsert:    "insert",
	input.KeyDelete:    "delete",
	input.KeyF1:        "f1",
	input.KeyF2:        "f2",
	input.KeyF3:        "f3",
	input.KeyF4:        "f4",
	input.KeyF5:        "f5",
	input.KeyF6:        "f6",
	input.KeyF7:        "f7",
	input.KeyF8:        "f8",
	input.KeyF9:        "f9",
	input.KeyF10:       "f10",
	input.KeyF11:       "f11",
	input.KeyF12:       "f12",
}

// mouseButtonName maps mouse buttons to string names.
var mouseButtonName = map[ansi.MouseButton]string{
	ansi.MouseNone:      "none",
	ansi.MouseLeft:      "left",
	ansi.MouseMiddle:    "middle",
	ansi.MouseRight:     "right",
	ansi.MouseWheelUp:   "wheel_up",
	ansi.MouseWheelDown: "wheel_down",
}

// ConvertInputEvent converts a charmbracelet/x/input event to a TTYEvent.
// Returns nil for unrecognized events.
func ConvertInputEvent(ev input.Event) *TTYEvent {
	switch e := ev.(type) {
	case input.KeyPressEvent:
		return convertKeyEvent(input.Key(e), "press")
	case input.KeyReleaseEvent:
		return convertKeyEvent(input.Key(e), "release")
	case input.MouseClickEvent:
		return convertMouseEvent(input.Mouse(e), "press")
	case input.MouseReleaseEvent:
		return convertMouseEvent(input.Mouse(e), "release")
	case input.MouseMotionEvent:
		return convertMouseEvent(input.Mouse(e), "motion")
	case input.MouseWheelEvent:
		return convertMouseEvent(input.Mouse(e), "wheel")
	case input.WindowSizeEvent:
		return &TTYEvent{
			Type:   "resize",
			Width:  e.Width,
			Height: e.Height,
		}
	case input.FocusEvent:
		return &TTYEvent{Type: "focus", Focused: true}
	case input.BlurEvent:
		return &TTYEvent{Type: "focus", Focused: false}
	case input.PasteEvent:
		return &TTYEvent{Type: "paste", Paste: string(e)}
	default:
		return nil
	}
}

// convertUVInputEvent adapts ultraviolet's streaming decoder events to the
// established terminal event contract.
func convertUVInputEvent(ev uv.Event) *TTYEvent {
	switch e := ev.(type) {
	case uv.KeyPressEvent:
		return convertUVKeyEvent(uv.Key(e), "press")
	case uv.KeyReleaseEvent:
		return convertUVKeyEvent(uv.Key(e), "release")
	case uv.MouseClickEvent:
		return convertUVMouseEvent(uv.Mouse(e), "press")
	case uv.MouseReleaseEvent:
		return convertUVMouseEvent(uv.Mouse(e), "release")
	case uv.MouseMotionEvent:
		return convertUVMouseEvent(uv.Mouse(e), "motion")
	case uv.MouseWheelEvent:
		return convertUVMouseEvent(uv.Mouse(e), "wheel")
	case uv.WindowSizeEvent:
		return &TTYEvent{Type: "resize", Width: e.Width, Height: e.Height}
	case uv.FocusEvent:
		return &TTYEvent{Type: "focus", Focused: true}
	case uv.BlurEvent:
		return &TTYEvent{Type: "focus", Focused: false}
	case uv.PasteEvent:
		return &TTYEvent{Type: "paste", Paste: e.Content}
	default:
		return nil
	}
}

func convertKeyEvent(k input.Key, action string) *TTYEvent {
	ev := &TTYEvent{
		Type:   "key",
		Action: action,
		Alt:    k.Mod.Contains(input.ModAlt),
		Ctrl:   k.Mod.Contains(input.ModCtrl),
		Shift:  k.Mod.Contains(input.ModShift),
	}

	if name, ok := keyTypeName[k.Code]; ok {
		ev.KeyType = name
		ev.Key = name
	} else if k.Text != "" {
		ev.KeyType = "runes"
		ev.Key = k.Text
	} else if k.Code > 0 && k.Code < input.KeyExtended {
		ev.KeyType = "runes"
		ev.Key = string(k.Code)
	} else {
		ev.KeyType = "unknown"
		ev.Key = k.Keystroke()
	}

	return ev
}

func convertMouseEvent(m input.Mouse, action string) *TTYEvent {
	btn := "none"
	if name, ok := mouseButtonName[m.Button]; ok {
		btn = name
	}
	return &TTYEvent{
		Type:   "mouse",
		Action: action,
		Button: btn,
		// Terminal surfaces and Lua arrays use one-based cell coordinates.
		// x/input deliberately exposes decoded protocol positions as zero-based.
		X:     m.X + 1,
		Y:     m.Y + 1,
		Alt:   m.Mod.Contains(input.ModAlt),
		Ctrl:  m.Mod.Contains(input.ModCtrl),
		Shift: m.Mod.Contains(input.ModShift),
	}
}

func convertUVKeyEvent(k uv.Key, action string) *TTYEvent {
	ev := &TTYEvent{
		Type:   "key",
		Action: action,
		Alt:    k.Mod.Contains(uv.ModAlt),
		Ctrl:   k.Mod.Contains(uv.ModCtrl),
		Shift:  k.Mod.Contains(uv.ModShift),
	}

	if name := uvKeyTypeName(k.Code); name != "" {
		ev.KeyType = name
		ev.Key = name
	} else if k.Text != "" {
		ev.KeyType = "runes"
		ev.Key = k.Text
	} else if k.Code > 0 && k.Code < uv.KeyExtended {
		ev.KeyType = "runes"
		ev.Key = string(k.Code)
	} else {
		ev.KeyType = "unknown"
		ev.Key = k.Keystroke()
	}
	return ev
}

func uvKeyTypeName(code rune) string {
	switch code {
	case uv.KeyEnter:
		return "enter"
	case uv.KeyTab:
		return "tab"
	case uv.KeyBackspace:
		return "backspace"
	case uv.KeyEscape:
		return "esc"
	case uv.KeySpace:
		return "space"
	case uv.KeyUp:
		return "up"
	case uv.KeyDown:
		return "down"
	case uv.KeyLeft:
		return "left"
	case uv.KeyRight:
		return "right"
	case uv.KeyHome:
		return "home"
	case uv.KeyEnd:
		return "end"
	case uv.KeyPgUp:
		return "pgup"
	case uv.KeyPgDown:
		return "pgdown"
	case uv.KeyInsert:
		return "insert"
	case uv.KeyDelete:
		return "delete"
	case uv.KeyF1:
		return "f1"
	case uv.KeyF2:
		return "f2"
	case uv.KeyF3:
		return "f3"
	case uv.KeyF4:
		return "f4"
	case uv.KeyF5:
		return "f5"
	case uv.KeyF6:
		return "f6"
	case uv.KeyF7:
		return "f7"
	case uv.KeyF8:
		return "f8"
	case uv.KeyF9:
		return "f9"
	case uv.KeyF10:
		return "f10"
	case uv.KeyF11:
		return "f11"
	case uv.KeyF12:
		return "f12"
	default:
		return ""
	}
}

func convertUVMouseEvent(m uv.Mouse, action string) *TTYEvent {
	btn := "none"
	if name, ok := mouseButtonName[m.Button]; ok {
		btn = name
	}
	return &TTYEvent{
		Type:   "mouse",
		Action: action,
		Button: btn,
		X:      m.X + 1,
		Y:      m.Y + 1,
		Alt:    m.Mod.Contains(uv.ModAlt),
		Ctrl:   m.Mod.Contains(uv.ModCtrl),
		Shift:  m.Mod.Contains(uv.ModShift),
	}
}
