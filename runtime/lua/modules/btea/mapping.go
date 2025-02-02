package btea

import tea "github.com/charmbracelet/bubbletea"

// Initialize key mappings
var (
	keyTypeMap = map[tea.KeyType]string{
		// Control keys using tea constants
		tea.KeyCtrlAt:           "ctrl+@", // null, \0
		tea.KeyCtrlA:            "ctrl+a",
		tea.KeyCtrlB:            "ctrl+b",
		tea.KeyCtrlC:            "ctrl+c",
		tea.KeyCtrlD:            "ctrl+d",
		tea.KeyCtrlE:            "ctrl+e",
		tea.KeyCtrlF:            "ctrl+f",
		tea.KeyCtrlG:            "ctrl+g",
		tea.KeyCtrlH:            "ctrl+h",
		tea.KeyCtrlJ:            "ctrl+j",
		tea.KeyCtrlK:            "ctrl+k",
		tea.KeyCtrlL:            "ctrl+l",
		tea.KeyCtrlN:            "ctrl+n",
		tea.KeyCtrlO:            "ctrl+o",
		tea.KeyCtrlP:            "ctrl+p",
		tea.KeyCtrlQ:            "ctrl+q",
		tea.KeyCtrlR:            "ctrl+r",
		tea.KeyCtrlS:            "ctrl+s",
		tea.KeyCtrlT:            "ctrl+t",
		tea.KeyCtrlU:            "ctrl+u",
		tea.KeyCtrlV:            "ctrl+v",
		tea.KeyCtrlW:            "ctrl+w",
		tea.KeyCtrlX:            "ctrl+x",
		tea.KeyCtrlY:            "ctrl+y",
		tea.KeyCtrlZ:            "ctrl+z",
		tea.KeyCtrlBackslash:    "ctrl+\\", // ctrl+\
		tea.KeyCtrlCloseBracket: "ctrl+]",  // ctrl+]
		tea.KeyCtrlCaret:        "ctrl+^",  // ctrl+^
		tea.KeyCtrlUnderscore:   "ctrl+_",  // ctrl+_

		// Navigation keys
		tea.KeyUp:        "up",
		tea.KeyDown:      "down",
		tea.KeyRight:     "right",
		tea.KeyLeft:      "left",
		tea.KeyHome:      "home",
		tea.KeyEnd:       "end",
		tea.KeyPgUp:      "pgup",
		tea.KeyPgDown:    "pgdown",
		tea.KeyTab:       "tab",
		tea.KeyBackspace: "backspace",
		tea.KeyDelete:    "delete",
		tea.KeyInsert:    "insert",
		tea.KeySpace:     "space",
		tea.KeyEnter:     "enter",
		tea.KeyEscape:    "esc",
		tea.KeyRunes:     "runes",

		// Shifted variants
		tea.KeyShiftTab:   "shift+tab",
		tea.KeyShiftUp:    "shift+up",
		tea.KeyShiftDown:  "shift+down",
		tea.KeyShiftLeft:  "shift+left",
		tea.KeyShiftRight: "shift+right",
		tea.KeyShiftHome:  "shift+home",
		tea.KeyShiftEnd:   "shift+end",

		// Ctrl variants
		tea.KeyCtrlUp:     "ctrl+up",
		tea.KeyCtrlDown:   "ctrl+down",
		tea.KeyCtrlRight:  "ctrl+right",
		tea.KeyCtrlLeft:   "ctrl+left",
		tea.KeyCtrlHome:   "ctrl+home",
		tea.KeyCtrlEnd:    "ctrl+end",
		tea.KeyCtrlPgUp:   "ctrl+pgup",
		tea.KeyCtrlPgDown: "ctrl+pgdown",

		// Ctrl+Shift variants
		tea.KeyCtrlShiftUp:    "ctrl+shift+up",
		tea.KeyCtrlShiftDown:  "ctrl+shift+down",
		tea.KeyCtrlShiftLeft:  "ctrl+shift+left",
		tea.KeyCtrlShiftRight: "ctrl+shift+right",
		tea.KeyCtrlShiftHome:  "ctrl+shift+home",
		tea.KeyCtrlShiftEnd:   "ctrl+shift+end",

		// Function keys
		tea.KeyF1:  "f1",
		tea.KeyF2:  "f2",
		tea.KeyF3:  "f3",
		tea.KeyF4:  "f4",
		tea.KeyF5:  "f5",
		tea.KeyF6:  "f6",
		tea.KeyF7:  "f7",
		tea.KeyF8:  "f8",
		tea.KeyF9:  "f9",
		tea.KeyF10: "f10",
		tea.KeyF11: "f11",
		tea.KeyF12: "f12",
		tea.KeyF13: "f13",
		tea.KeyF14: "f14",
		tea.KeyF15: "f15",
		tea.KeyF16: "f16",
		tea.KeyF17: "f17",
		tea.KeyF18: "f18",
		tea.KeyF19: "f19",
		tea.KeyF20: "f20",
	}

	// Mouse buttons
	mouseButtonMap = map[tea.MouseButton]string{
		tea.MouseButtonNone:       "none",
		tea.MouseButtonLeft:       "left",
		tea.MouseButtonMiddle:     "middle",
		tea.MouseButtonRight:      "right",
		tea.MouseButtonWheelUp:    "wheel_up",
		tea.MouseButtonWheelDown:  "wheel_down",
		tea.MouseButtonWheelLeft:  "wheel_left",
		tea.MouseButtonWheelRight: "wheel_right",
		tea.MouseButtonBackward:   "backward",
		tea.MouseButtonForward:    "forward",
		tea.MouseButton10:         "button10",
		tea.MouseButton11:         "button11",
	}
	keyTypeFromStr     = make(map[string]tea.KeyType)
	mouseButtonFromStr = make(map[string]tea.MouseButton)
)

// Initialize reverse mappings
func init() {
	// Generate reverse key mapping
	keyTypeFromStr = make(map[string]tea.KeyType, len(keyTypeMap))
	for k, v := range keyTypeMap {
		keyTypeFromStr[v] = k
	}

	// Generate reverse mouse mapping
	mouseButtonFromStr = make(map[string]tea.MouseButton, len(mouseButtonMap))
	for k, v := range mouseButtonMap {
		mouseButtonFromStr[v] = k
	}
}
