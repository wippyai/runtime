// SPDX-License-Identifier: MPL-2.0

package input

import (
	"strconv"
	"strings"
	"unicode"
)

// Event is a decoded terminal input event. The concrete types are Key press
// and release events, mouse events, focus changes, bracketed paste content,
// window size changes, kitty graphics replies, and unrecognized sequences.
type Event interface {
	isEvent()
}

// Mod is a set of keyboard and mouse modifiers.
type Mod int

// Modifier flags. Lock states are reported by the kitty keyboard protocol and
// by the Windows console.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModMeta
	ModHyper
	ModSuper
	ModCapsLock
	ModNumLock
	ModScrollLock
)

// Contains reports whether every modifier in mods is set.
func (m Mod) Contains(mods Mod) bool { return m&mods == mods }

// Key describes one keyboard key.
type Key struct {
	// Text is the text the key produces, empty for non-printing keys and
	// modified keys.
	Text string
	// Code is the key identity: a rune for character keys or one of the Key
	// constants for named keys.
	Code rune
	// ShiftedCode is the key identity with Shift applied, reported by the kitty
	// keyboard protocol and the Windows console.
	ShiftedCode rune
	// BaseCode is the key identity on a standard PC-101 layout, reported by the
	// kitty keyboard protocol and the Windows console.
	BaseCode rune
	Mod      Mod
	// IsRepeat marks an auto-repeated press.
	IsRepeat bool
}

// Keystroke returns a stable textual form such as "ctrl+alt+up" or "shift+a".
func (k Key) Keystroke() string {
	var b strings.Builder
	for _, m := range []struct {
		name string
		mod  Mod
	}{
		{"ctrl", ModCtrl}, {"alt", ModAlt}, {"shift", ModShift},
		{"meta", ModMeta}, {"hyper", ModHyper}, {"super", ModSuper},
	} {
		if k.Mod.Contains(m.mod) {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	if name, ok := keyNames[k.Code]; ok {
		b.WriteString(name)
	} else if k.Code > 0 && k.Code < KeyExtended && unicode.IsPrint(k.Code) {
		b.WriteRune(k.Code)
	} else if k.Text != "" {
		b.WriteString(k.Text)
	} else {
		b.WriteString("0x" + strconv.FormatInt(int64(k.Code), 16))
	}
	return b.String()
}

// KeyPressEvent is a key press, including auto-repeat.
type KeyPressEvent Key

// KeyReleaseEvent is a key release, reported by the kitty keyboard protocol
// and the Windows console.
type KeyReleaseEvent Key

// MouseButton identifies a mouse button or wheel direction.
type MouseButton int

// Mouse buttons.
const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
	MouseBackward
	MouseForward
	MouseButton10
	MouseButton11
)

// Mouse is a pointer event. X and Y are zero-based cell coordinates.
type Mouse struct {
	X      int
	Y      int
	Button MouseButton
	Mod    Mod
}

// MouseClickEvent is a button press.
type MouseClickEvent Mouse

// MouseReleaseEvent is a button release.
type MouseReleaseEvent Mouse

// MouseMotionEvent is pointer movement, with the held button if any.
type MouseMotionEvent Mouse

// MouseWheelEvent is a wheel step.
type MouseWheelEvent Mouse

// FocusEvent reports that the terminal gained focus.
type FocusEvent struct{}

// BlurEvent reports that the terminal lost focus.
type BlurEvent struct{}

// PasteEvent is the content of one bracketed paste.
type PasteEvent string

// WindowSizeEvent reports the terminal size in cells.
type WindowSizeEvent struct {
	Width  int
	Height int
}

// GraphicsEvent is a kitty graphics protocol reply: APC G <options> ; <payload>.
type GraphicsEvent struct {
	Payload []byte
	// ID is the image id (key "i") from the reply options, zero when absent.
	ID int
}

// UnknownEvent carries the bytes of a sequence the decoder recognizes as
// well-formed but does not interpret, or malformed input it discards.
type UnknownEvent string

func (KeyPressEvent) isEvent()     {}
func (KeyReleaseEvent) isEvent()   {}
func (MouseClickEvent) isEvent()   {}
func (MouseReleaseEvent) isEvent() {}
func (MouseMotionEvent) isEvent()  {}
func (MouseWheelEvent) isEvent()   {}
func (FocusEvent) isEvent()        {}
func (BlurEvent) isEvent()         {}
func (PasteEvent) isEvent()        {}
func (WindowSizeEvent) isEvent()   {}
func (GraphicsEvent) isEvent()     {}
func (UnknownEvent) isEvent()      {}
