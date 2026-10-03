// SPDX-License-Identifier: MPL-2.0

package input

import (
	"unicode"
	"unicode/utf16"
)

// Windows console control key state flags.
const (
	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
	shiftPressed     = 0x0010
	numLockOn        = 0x0020
	scrollLockOn     = 0x0040
	capsLockOn       = 0x0080
	enhancedKey      = 0x0100
)

// Windows console mouse button state and event flags.
const (
	fromLeft1stButtonPressed = 0x0001
	rightmostButtonPressed   = 0x0002
	fromLeft2ndButtonPressed = 0x0004
	fromLeft3rdButtonPressed = 0x0008
	fromLeft4thButtonPressed = 0x0010

	mouseMoved    = 0x0001
	doubleClick   = 0x0002
	mouseWheeled  = 0x0004
	mouseHWheeled = 0x0008
)

// Windows virtual key codes.
const (
	vkBack      = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0d
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkPause     = 0x13
	vkCapital   = 0x14
	vkEscape    = 0x1b
	vkSpace     = 0x20
	vkPrior     = 0x21
	vkNext      = 0x22
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkSelect    = 0x29
	vkSnapshot  = 0x2c
	vkInsert    = 0x2d
	vkDelete    = 0x2e
	vkLWin      = 0x5b
	vkRWin      = 0x5c
	vkApps      = 0x5d
	vkNumpad0   = 0x60
	vkNumpad9   = 0x69
	vkMultiply  = 0x6a
	vkAdd       = 0x6b
	vkSeparator = 0x6c
	vkSubtract  = 0x6d
	vkDecimal   = 0x6e
	vkDivide    = 0x6f
	vkF1        = 0x70
	vkF24       = 0x87
	vkNumLock   = 0x90
	vkScroll    = 0x91
	vkLShift    = 0xa0
	vkRShift    = 0xa1
	vkLControl  = 0xa2
	vkRControl  = 0xa3
	vkLMenu     = 0xa4
	vkRMenu     = 0xa5
	vkVolMute   = 0xad
	vkVolDown   = 0xae
	vkVolUp     = 0xaf
	vkMediaNext = 0xb0
	vkMediaPrev = 0xb1
	vkMediaStop = 0xb2
	vkMediaPlay = 0xb3
	vkOem1      = 0xba
	vkOemPlus   = 0xbb
	vkOemComma  = 0xbc
	vkOemMinus  = 0xbd
	vkOemPeriod = 0xbe
	vkOem2      = 0xbf
	vkOem3      = 0xc0
	vkOem4      = 0xdb
	vkOem5      = 0xdc
	vkOem6      = 0xdd
	vkOem7      = 0xde
)

// KeyRecord is the content of a Windows console KEY_EVENT_RECORD.
type KeyRecord struct {
	ControlKeyState uint32
	RepeatCount     uint16
	VirtualKey      uint16
	ScanCode        uint16
	Char            uint16
	KeyDown         bool
}

// MouseRecord is the content of a Windows console MOUSE_EVENT_RECORD.
type MouseRecord struct {
	ButtonState     uint32
	ControlKeyState uint32
	EventFlags      uint32
	X               int16
	Y               int16
}

// ConsoleDecoder converts Windows console input records into events. It keeps
// the state that spans records: UTF-16 surrogate halves, the previous
// modifier state, the previous mouse buttons, and the last reported size. It
// is not safe for concurrent use.
type ConsoleDecoder struct {
	utf16Buf      [2]rune
	utf16Half     bool
	lastCks       uint32
	lastMouseBtns uint32
	lastW, lastH  int16
}

// Key decodes a key record. A repeat count above one yields that many events.
func (d *ConsoleDecoder) Key(r KeyRecord) []Event {
	ev := d.key(r)
	if ev == nil {
		return nil
	}
	count := max(int(r.RepeatCount), 1)
	out := make([]Event, count)
	for i := range out {
		out[i] = ev
	}
	return out
}

func (d *ConsoleDecoder) key(r KeyRecord) Event {
	cks := r.ControlKeyState
	defer func() { d.lastCks = cks }()
	char := rune(r.Char)

	if d.utf16Half {
		d.utf16Half = false
		cp := utf16.DecodeRune(d.utf16Buf[0], char)
		k := Key{Code: cp, Text: string(cp), Mod: consoleMod(cks)}
		return keyEvent(consoleCase(k, cks), r.KeyDown)
	}
	if utf16.IsSurrogate(char) {
		d.utf16Buf[0] = char
		d.utf16Half = true
		return nil
	}

	base := d.baseCode(r.VirtualKey, char, cks)

	// AltGr is reported as left Ctrl plus right Alt and types printable text.
	altGr := cks&(leftCtrlPressed|rightAltPressed) == leftCtrlPressed|rightAltPressed
	code := base
	var text string
	if !unicode.IsControl(char) && char != 0 {
		code = char
		mods := cks &^ (enhancedKey | numLockOn | scrollLockOn)
		if unicode.IsPrint(code) && (mods&^(shiftPressed|capsLockOn) == 0 || altGr) {
			text = string(code)
		}
	}
	k := Key{Code: code, Text: text, BaseCode: base, Mod: consoleMod(cks)}
	return keyEvent(consoleCase(k, cks), r.KeyDown)
}

func keyEvent(k Key, down bool) Event {
	if down {
		return KeyPressEvent(k)
	}
	return KeyReleaseEvent(k)
}

func (d *ConsoleDecoder) baseCode(vk uint16, char rune, cks uint32) rune {
	last := d.lastCks
	pick := func(left, right uint32, lk, rk rune) rune {
		switch {
		case cks&left != 0:
			return lk
		case cks&right != 0:
			return rk
		case last&left != 0:
			return lk
		case last&right != 0:
			return rk
		}
		return 0
	}
	switch {
	case vk == 0:
		return char
	case vk == vkBack:
		return KeyBackspace
	case vk == vkTab:
		return KeyTab
	case vk == vkReturn:
		return KeyEnter
	case vk == vkShift:
		for _, c := range [...]uint32{cks, last} {
			if c&shiftPressed != 0 {
				if c&enhancedKey != 0 {
					return KeyRightShift
				}
				return KeyLeftShift
			}
		}
		return 0
	case vk == vkControl:
		return pick(leftCtrlPressed, rightCtrlPressed, KeyLeftCtrl, KeyRightCtrl)
	case vk == vkMenu:
		return pick(leftAltPressed, rightAltPressed, KeyLeftAlt, KeyRightAlt)
	case vk == vkPause:
		return KeyPause
	case vk == vkCapital:
		return KeyCapsLock
	case vk == vkEscape:
		return KeyEscape
	case vk == vkSpace:
		return KeySpace
	case vk == vkPrior:
		return KeyPgUp
	case vk == vkNext:
		return KeyPgDown
	case vk == vkEnd:
		return KeyEnd
	case vk == vkHome:
		return KeyHome
	case vk == vkLeft:
		return KeyLeft
	case vk == vkUp:
		return KeyUp
	case vk == vkRight:
		return KeyRight
	case vk == vkDown:
		return KeyDown
	case vk == vkSelect:
		return KeySelect
	case vk == vkSnapshot:
		return KeyPrintScreen
	case vk == vkInsert:
		return KeyInsert
	case vk == vkDelete:
		return KeyDelete
	case vk >= '0' && vk <= '9':
		return rune(vk)
	case vk >= 'A' && vk <= 'Z':
		return rune(vk) + 32
	case vk == vkLWin:
		return KeyLeftSuper
	case vk == vkRWin:
		return KeyRightSuper
	case vk == vkApps:
		return KeyMenu
	case vk >= vkNumpad0 && vk <= vkNumpad9:
		return KeyKp0 + rune(vk-vkNumpad0)
	case vk == vkMultiply:
		return KeyKpMultiply
	case vk == vkAdd:
		return KeyKpPlus
	case vk == vkSeparator:
		return KeyKpComma
	case vk == vkSubtract:
		return KeyKpMinus
	case vk == vkDecimal:
		return KeyKpDecimal
	case vk == vkDivide:
		return KeyKpDivide
	case vk >= vkF1 && vk <= vkF24:
		return KeyF1 + rune(vk-vkF1)
	case vk == vkNumLock:
		return KeyNumLock
	case vk == vkScroll:
		return KeyScrollLock
	case vk == vkLShift:
		return KeyLeftShift
	case vk == vkRShift:
		return KeyRightShift
	case vk == vkLControl:
		return KeyLeftCtrl
	case vk == vkRControl:
		return KeyRightCtrl
	case vk == vkLMenu:
		return KeyLeftAlt
	case vk == vkRMenu:
		return KeyRightAlt
	case vk == vkVolMute:
		return KeyMute
	case vk == vkVolDown:
		return KeyLowerVol
	case vk == vkVolUp:
		return KeyRaiseVol
	case vk == vkMediaNext:
		return KeyMediaNext
	case vk == vkMediaPrev:
		return KeyMediaPrev
	case vk == vkMediaStop:
		return KeyMediaStop
	case vk == vkMediaPlay:
		return KeyMediaPlayPause
	}
	return oemCode(vk)
}

func oemCode(vk uint16) rune {
	switch vk {
	case vkOem1:
		return ';'
	case vkOemPlus:
		return '+'
	case vkOemComma:
		return ','
	case vkOemMinus:
		return '-'
	case vkOemPeriod:
		return '.'
	case vkOem2:
		return '/'
	case vkOem3:
		return '`'
	case vkOem4:
		return '['
	case vkOem5:
		return '\\'
	case vkOem6:
		return ']'
	case vkOem7:
		return '\''
	}
	return 0
}

// consoleCase follows the kitty model: an upper-case character reports the
// lower-case key as Code and the character as ShiftedCode.
func consoleCase(k Key, cks uint32) Key {
	if k.Text == "" {
		return k
	}
	if cks&(shiftPressed|capsLockOn) != 0 {
		switch {
		case unicode.IsUpper(k.Code):
			k.ShiftedCode = k.Code
			k.Code = unicode.ToLower(k.Code)
		case unicode.IsLower(k.Code):
			k.ShiftedCode = unicode.ToUpper(k.Code)
			k.Text = string(k.ShiftedCode)
		}
	} else if unicode.IsUpper(k.Code) {
		k.ShiftedCode = unicode.ToLower(k.Code)
		k.Text = string(k.ShiftedCode)
	}
	return k
}

func consoleMod(cks uint32) Mod {
	var m Mod
	if cks&(leftCtrlPressed|rightCtrlPressed) != 0 {
		m |= ModCtrl
	}
	if cks&(leftAltPressed|rightAltPressed) != 0 {
		m |= ModAlt
	}
	if cks&shiftPressed != 0 {
		m |= ModShift
	}
	if cks&capsLockOn != 0 {
		m |= ModCapsLock
	}
	if cks&numLockOn != 0 {
		m |= ModNumLock
	}
	if cks&scrollLockOn != 0 {
		m |= ModScrollLock
	}
	return m
}

// Mouse decodes a mouse record. Button releases are derived from the change
// against the previous button state.
func (d *ConsoleDecoder) Mouse(r MouseRecord) Event {
	m := Mouse{X: int(r.X), Y: int(r.Y)}
	if r.ControlKeyState&(leftAltPressed|rightAltPressed) != 0 {
		m.Mod |= ModAlt
	}
	if r.ControlKeyState&(leftCtrlPressed|rightCtrlPressed) != 0 {
		m.Mod |= ModCtrl
	}
	if r.ControlKeyState&shiftPressed != 0 {
		m.Mod |= ModShift
	}

	prev := d.lastMouseBtns
	d.lastMouseBtns = r.ButtonState

	wheelUp := int16(r.ButtonState>>16) > 0
	var release bool
	switch r.EventFlags {
	case 0, doubleClick:
		m.Button, release = consoleButton(prev, r.ButtonState)
	case mouseWheeled:
		m.Button = MouseWheelDown
		if wheelUp {
			m.Button = MouseWheelUp
		}
	case mouseHWheeled:
		m.Button = MouseWheelLeft
		if wheelUp {
			m.Button = MouseWheelRight
		}
	case mouseMoved:
		m.Button, _ = consoleButton(prev, r.ButtonState)
		return MouseMotionEvent(m)
	}
	switch {
	case isWheel(m.Button):
		return MouseWheelEvent(m)
	case release:
		return MouseReleaseEvent(m)
	}
	return MouseClickEvent(m)
}

func consoleButton(prev, state uint32) (MouseButton, bool) {
	changed := prev ^ state
	release := changed&state == 0
	if changed == 0 {
		changed = state
		release = true
	}
	switch {
	case changed&fromLeft1stButtonPressed != 0:
		return MouseLeft, release
	case changed&fromLeft2ndButtonPressed != 0:
		return MouseMiddle, release
	case changed&rightmostButtonPressed != 0:
		return MouseRight, release
	case changed&fromLeft3rdButtonPressed != 0:
		return MouseBackward, release
	case changed&fromLeft4thButtonPressed != 0:
		return MouseForward, release
	}
	return MouseNone, release
}

// Size decodes a window buffer size record, reporting only changes.
func (d *ConsoleDecoder) Size(width, height int16) Event {
	if width == d.lastW && height == d.lastH {
		return nil
	}
	d.lastW, d.lastH = width, height
	return WindowSizeEvent{Width: int(width), Height: int(height)}
}

// Focus decodes a focus record.
func Focus(set bool) Event {
	if set {
		return FocusEvent{}
	}
	return BlurEvent{}
}

// win32Key carries a win32-input-mode key sequence (CSI Vk;Sc;Uc;Kd;Cs;Rc _)
// to the parser, whose console decoder resolves it.
type win32Key struct{ rec KeyRecord }

func (win32Key) isEvent() {}

func win32InputKey(c *csi) Event {
	return win32Key{rec: KeyRecord{
		VirtualKey:      uint16(c.param(0, 0, 0)),
		ScanCode:        uint16(c.param(1, 0, 0)),
		Char:            uint16(c.param(2, 0, 0)),
		KeyDown:         c.param(3, 0, 0) == 1,
		ControlKeyState: uint32(c.param(4, 0, 0)),
		RepeatCount:     uint16(c.param(5, 0, 1)),
	}}
}
