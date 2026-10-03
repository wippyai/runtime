// SPDX-License-Identifier: MPL-2.0

package input

import (
	"strconv"
	"unicode"
)

// KeyExtended is the first code above the Unicode range. Named keys live at
// and above it; a multi-rune grapheme cluster is reported with this code and
// the cluster in Key.Text.
const KeyExtended = unicode.MaxRune + 1

// Named keys outside the Unicode range.
const (
	KeyUp rune = KeyExtended + 1 + iota
	KeyDown
	KeyRight
	KeyLeft
	KeyBegin
	KeyFind
	KeyInsert
	KeyDelete
	KeySelect
	KeyPgUp
	KeyPgDown
	KeyHome
	KeyEnd

	KeyKpEnter
	KeyKpEqual
	KeyKpMultiply
	KeyKpPlus
	KeyKpComma
	KeyKpMinus
	KeyKpDecimal
	KeyKpDivide
	KeyKp0
	KeyKp1
	KeyKp2
	KeyKp3
	KeyKp4
	KeyKp5
	KeyKp6
	KeyKp7
	KeyKp8
	KeyKp9
	KeyKpSep
	KeyKpUp
	KeyKpDown
	KeyKpLeft
	KeyKpRight
	KeyKpPgUp
	KeyKpPgDown
	KeyKpHome
	KeyKpEnd
	KeyKpInsert
	KeyKpDelete
	KeyKpBegin

	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	KeyF21
	KeyF22
	KeyF23
	KeyF24
	KeyF25
	KeyF26
	KeyF27
	KeyF28
	KeyF29
	KeyF30
	KeyF31
	KeyF32
	KeyF33
	KeyF34
	KeyF35

	KeyCapsLock
	KeyScrollLock
	KeyNumLock
	KeyPrintScreen
	KeyPause
	KeyMenu

	KeyMediaPlay
	KeyMediaPause
	KeyMediaPlayPause
	KeyMediaReverse
	KeyMediaStop
	KeyMediaFastForward
	KeyMediaRewind
	KeyMediaNext
	KeyMediaPrev
	KeyMediaRecord

	KeyLowerVol
	KeyRaiseVol
	KeyMute

	KeyLeftShift
	KeyLeftAlt
	KeyLeftCtrl
	KeyLeftSuper
	KeyLeftHyper
	KeyLeftMeta
	KeyRightShift
	KeyRightAlt
	KeyRightCtrl
	KeyRightSuper
	KeyRightHyper
	KeyRightMeta
	KeyIsoLevel3Shift
	KeyIsoLevel5Shift
)

// Control characters that are keys in their own right.
const (
	KeyBackspace rune = 0x7f
	KeyTab       rune = 0x09
	KeyEnter     rune = 0x0d
	KeyEscape    rune = 0x1b
	KeySpace     rune = 0x20
)

var keyNames = map[rune]string{
	KeyBackspace: "backspace", KeyTab: "tab", KeyEnter: "enter", KeyEscape: "esc", KeySpace: "space",
	KeyUp: "up", KeyDown: "down", KeyRight: "right", KeyLeft: "left", KeyBegin: "begin",
	KeyFind: "find", KeyInsert: "insert", KeyDelete: "delete", KeySelect: "select",
	KeyPgUp: "pgup", KeyPgDown: "pgdown", KeyHome: "home", KeyEnd: "end",
	KeyKpEnter: "kpenter", KeyKpEqual: "kpequal", KeyKpMultiply: "kpmul", KeyKpPlus: "kpplus",
	KeyKpComma: "kpcomma", KeyKpMinus: "kpminus", KeyKpDecimal: "kpperiod", KeyKpDivide: "kpdiv",
	KeyKp0: "kp0", KeyKp1: "kp1", KeyKp2: "kp2", KeyKp3: "kp3", KeyKp4: "kp4",
	KeyKp5: "kp5", KeyKp6: "kp6", KeyKp7: "kp7", KeyKp8: "kp8", KeyKp9: "kp9",
	KeyKpSep: "kpsep", KeyKpUp: "kpup", KeyKpDown: "kpdown", KeyKpLeft: "kpleft",
	KeyKpRight: "kpright", KeyKpPgUp: "kppgup", KeyKpPgDown: "kppgdown", KeyKpHome: "kphome",
	KeyKpEnd: "kpend", KeyKpInsert: "kpinsert", KeyKpDelete: "kpdelete", KeyKpBegin: "kpbegin",
	KeyCapsLock: "capslock", KeyScrollLock: "scrolllock", KeyNumLock: "numlock",
	KeyPrintScreen: "printscreen", KeyPause: "pause", KeyMenu: "menu",
	KeyMediaPlay: "mediaplay", KeyMediaPause: "mediapause", KeyMediaPlayPause: "mediaplaypause",
	KeyMediaReverse: "mediareverse", KeyMediaStop: "mediastop", KeyMediaFastForward: "mediafastforward",
	KeyMediaRewind: "mediarewind", KeyMediaNext: "medianext", KeyMediaPrev: "mediaprev",
	KeyMediaRecord: "mediarecord", KeyLowerVol: "volumedown", KeyRaiseVol: "volumeup", KeyMute: "mute",
	KeyLeftShift: "leftshift", KeyLeftAlt: "leftalt", KeyLeftCtrl: "leftctrl", KeyLeftSuper: "leftsuper",
	KeyLeftHyper: "lefthyper", KeyLeftMeta: "leftmeta", KeyRightShift: "rightshift",
	KeyRightAlt: "rightalt", KeyRightCtrl: "rightctrl", KeyRightSuper: "rightsuper",
	KeyRightHyper: "righthyper", KeyRightMeta: "rightmeta",
	KeyIsoLevel3Shift: "isolevel3shift", KeyIsoLevel5Shift: "isolevel5shift",
}

func init() {
	for i := 0; i < 35; i++ {
		keyNames[KeyF1+rune(i)] = "f" + strconv.Itoa(i+1)
	}
}

// KeyName returns the lowercase name of a named key, or "" for other codes.
func KeyName(code rune) string { return keyNames[code] }
