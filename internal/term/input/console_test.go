// SPDX-License-Identifier: MPL-2.0

package input

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsoleKeyText(t *testing.T) {
	var d ConsoleDecoder
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: 'a', BaseCode: 'a', Text: "a"}},
		d.Key(KeyRecord{KeyDown: true, VirtualKey: 'A', Char: 'a', RepeatCount: 1}))
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: 'a', BaseCode: 'a', Text: "a", Mod: ModNumLock}},
		d.Key(KeyRecord{KeyDown: true, VirtualKey: 'A', Char: 'a', ControlKeyState: numLockOn, RepeatCount: 1}))
	assert.Equal(t,
		[]Event{KeyReleaseEvent{Code: 'a', BaseCode: 'a', Text: "a"}},
		d.Key(KeyRecord{VirtualKey: 'A', Char: 'a', RepeatCount: 1}))
}

func TestConsoleKeyModifiersSuppressText(t *testing.T) {
	var d ConsoleDecoder
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: 'c', BaseCode: 'c', Mod: ModCtrl}},
		d.Key(KeyRecord{KeyDown: true, VirtualKey: 'C', Char: 3, ControlKeyState: leftCtrlPressed, RepeatCount: 1}))
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: 'x', BaseCode: 'x', Mod: ModAlt}},
		d.Key(KeyRecord{KeyDown: true, VirtualKey: 'X', Char: 0, ControlKeyState: leftAltPressed, RepeatCount: 1}))
	// AltGr (left Ctrl + right Alt) types text.
	altGr := uint32(leftCtrlPressed | rightAltPressed)
	got := d.Key(KeyRecord{KeyDown: true, VirtualKey: 'Q', Char: '@', ControlKeyState: altGr, RepeatCount: 1})
	assert.Equal(t, "@", got[0].(KeyPressEvent).Text)
}

func TestConsoleNamedKeys(t *testing.T) {
	var d ConsoleDecoder
	cases := map[uint16]rune{
		vkReturn: KeyEnter, vkTab: KeyTab, vkBack: KeyBackspace, vkEscape: KeyEscape,
		vkUp: KeyUp, vkDown: KeyDown, vkLeft: KeyLeft, vkRight: KeyRight,
		vkHome: KeyHome, vkEnd: KeyEnd, vkPrior: KeyPgUp, vkNext: KeyPgDown,
		vkInsert: KeyInsert, vkDelete: KeyDelete, vkF1: KeyF1, vkF1 + 11: KeyF12,
		vkNumpad0 + 3: KeyKp3, vkSpace: KeySpace,
	}
	for vk, want := range cases {
		got := d.Key(KeyRecord{KeyDown: true, VirtualKey: vk, RepeatCount: 1})
		if assert.Len(t, got, 1) {
			assert.Equal(t, want, got[0].(KeyPressEvent).Code, "vk %#x", vk)
		}
	}
}

func TestConsoleKeyRepeatCount(t *testing.T) {
	var d ConsoleDecoder
	assert.Len(t, d.Key(KeyRecord{KeyDown: true, VirtualKey: vkDown, RepeatCount: 3}), 3)
	assert.Len(t, d.Key(KeyRecord{KeyDown: true, VirtualKey: vkDown}), 1)
}

func TestConsoleModifierKeyUsesPreviousState(t *testing.T) {
	var d ConsoleDecoder
	pressed := d.Key(KeyRecord{KeyDown: true, VirtualKey: vkControl, ControlKeyState: leftCtrlPressed, RepeatCount: 1})
	assert.Equal(t, KeyLeftCtrl, pressed[0].(KeyPressEvent).Code)
	released := d.Key(KeyRecord{VirtualKey: vkControl, RepeatCount: 1})
	assert.Equal(t, KeyReleaseEvent{Code: KeyLeftCtrl, BaseCode: KeyLeftCtrl}, released[0])
}

func TestConsoleSurrogatePair(t *testing.T) {
	var d ConsoleDecoder
	assert.Empty(t, d.Key(KeyRecord{KeyDown: true, Char: 0xd83d, RepeatCount: 1}))
	got := d.Key(KeyRecord{KeyDown: true, Char: 0xde00, RepeatCount: 1})
	assert.Equal(t, []Event{KeyPressEvent{Code: '\U0001F600', Text: "\U0001F600"}}, got)
}

func TestConsoleMouse(t *testing.T) {
	var d ConsoleDecoder
	assert.Equal(t, MouseClickEvent{X: 3, Y: 4, Button: MouseLeft},
		d.Mouse(MouseRecord{X: 3, Y: 4, ButtonState: fromLeft1stButtonPressed}))
	assert.Equal(t, MouseMotionEvent{X: 5, Y: 4, Button: MouseLeft},
		d.Mouse(MouseRecord{X: 5, Y: 4, ButtonState: fromLeft1stButtonPressed, EventFlags: mouseMoved}))
	assert.Equal(t, MouseReleaseEvent{X: 5, Y: 4, Button: MouseLeft},
		d.Mouse(MouseRecord{X: 5, Y: 4}))
	assert.Equal(t, MouseClickEvent{Button: MouseRight, Mod: ModCtrl},
		d.Mouse(MouseRecord{ButtonState: rightmostButtonPressed, ControlKeyState: leftCtrlPressed}))
	d.Mouse(MouseRecord{})
	assert.Equal(t, MouseClickEvent{Button: MouseMiddle},
		d.Mouse(MouseRecord{ButtonState: fromLeft2ndButtonPressed}))
	d.Mouse(MouseRecord{})
	assert.Equal(t, MouseMotionEvent{X: 1, Y: 1, Button: MouseNone},
		d.Mouse(MouseRecord{X: 1, Y: 1, EventFlags: mouseMoved}))
	assert.Equal(t, MouseWheelEvent{Button: MouseWheelUp},
		d.Mouse(MouseRecord{ButtonState: 120 << 16, EventFlags: mouseWheeled}))
	assert.Equal(t, MouseWheelEvent{Button: MouseWheelDown},
		d.Mouse(MouseRecord{ButtonState: uint32(0xff88) << 16, EventFlags: mouseWheeled}))
	assert.Equal(t, MouseWheelEvent{Button: MouseWheelRight},
		d.Mouse(MouseRecord{ButtonState: 120 << 16, EventFlags: mouseHWheeled}))
}

func TestConsoleSizeAndFocus(t *testing.T) {
	var d ConsoleDecoder
	assert.Equal(t, WindowSizeEvent{Width: 80, Height: 24}, d.Size(80, 24))
	assert.Nil(t, d.Size(80, 24))
	assert.Equal(t, WindowSizeEvent{Width: 100, Height: 24}, d.Size(100, 24))
	assert.Equal(t, FocusEvent{}, Focus(true))
	assert.Equal(t, BlurEvent{}, Focus(false))
}
