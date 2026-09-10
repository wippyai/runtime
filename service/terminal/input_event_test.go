// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/input"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertUVInputEventPreservesNavigationAndKittyModifiers(t *testing.T) {
	navigation := convertUVInputEvent(uv.KeyPressEvent(uv.Key{Code: uv.KeyPgDown, Mod: uv.ModCtrl | uv.ModAlt}))
	require.NotNil(t, navigation)
	assert.Equal(t, "key", navigation.Type)
	assert.Equal(t, "pgdown", navigation.Key)
	assert.Equal(t, "pgdown", navigation.KeyType)
	assert.True(t, navigation.Ctrl)
	assert.True(t, navigation.Alt)

	kittyControl := convertUVInputEvent(uv.KeyReleaseEvent(uv.Key{
		Code:     'a',
		BaseCode: 'a',
		Mod:      uv.ModCtrl,
	}))
	require.NotNil(t, kittyControl)
	assert.Equal(t, "release", kittyControl.Action)
	assert.Equal(t, "a", kittyControl.Key)
	assert.Equal(t, "runes", kittyControl.KeyType)
	assert.True(t, kittyControl.Ctrl)

	kittyText := convertUVInputEvent(uv.KeyPressEvent(uv.Key{
		Code:        'é',
		BaseCode:    'e',
		ShiftedCode: 'É',
		Text:        "é",
		Mod:         uv.ModShift,
	}))
	require.NotNil(t, kittyText)
	assert.Equal(t, "é", kittyText.Key)
	assert.Equal(t, "runes", kittyText.KeyType)
	assert.True(t, kittyText.Shift)
}

func TestConvertInputEvent_KeyPress_Runes(t *testing.T) {
	ev := input.KeyPressEvent(input.Key{
		Code: 'a',
		Text: "a",
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "key", result.Type)
	assert.Equal(t, "a", result.Key)
	assert.Equal(t, "runes", result.KeyType)
	assert.False(t, result.Ctrl)
	assert.False(t, result.Alt)
	assert.False(t, result.Shift)
}

func TestConvertInputEvent_KeyPress_SpecialKey(t *testing.T) {
	tests := []struct {
		name    string
		keyType string
		code    rune
	}{
		{name: "enter", code: input.KeyEnter, keyType: "enter"},
		{name: "tab", code: input.KeyTab, keyType: "tab"},
		{name: "escape", code: input.KeyEscape, keyType: "esc"},
		{name: "backspace", code: input.KeyBackspace, keyType: "backspace"},
		{name: "up", code: input.KeyUp, keyType: "up"},
		{name: "down", code: input.KeyDown, keyType: "down"},
		{name: "left", code: input.KeyLeft, keyType: "left"},
		{name: "right", code: input.KeyRight, keyType: "right"},
		{name: "home", code: input.KeyHome, keyType: "home"},
		{name: "end", code: input.KeyEnd, keyType: "end"},
		{name: "pgup", code: input.KeyPgUp, keyType: "pgup"},
		{name: "pgdown", code: input.KeyPgDown, keyType: "pgdown"},
		{name: "delete", code: input.KeyDelete, keyType: "delete"},
		{name: "insert", code: input.KeyInsert, keyType: "insert"},
		{name: "f1", code: input.KeyF1, keyType: "f1"},
		{name: "f12", code: input.KeyF12, keyType: "f12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := input.KeyPressEvent(input.Key{Code: tt.code})
			result := ConvertInputEvent(ev)
			require.NotNil(t, result)
			assert.Equal(t, "key", result.Type)
			assert.Equal(t, tt.keyType, result.KeyType)
			assert.Equal(t, tt.keyType, result.Key)
		})
	}
}

func TestConvertInputEvent_KeyPress_Modifiers(t *testing.T) {
	ev := input.KeyPressEvent(input.Key{
		Code: 'c',
		Text: "c",
		Mod:  input.ModCtrl | input.ModAlt,
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "key", result.Type)
	assert.True(t, result.Ctrl)
	assert.True(t, result.Alt)
	assert.False(t, result.Shift)
}

func TestConvertInputEvent_MouseClick(t *testing.T) {
	ev := input.MouseClickEvent(input.Mouse{
		X:      10,
		Y:      20,
		Button: ansi.MouseLeft,
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "mouse", result.Type)
	assert.Equal(t, "press", result.Action)
	assert.Equal(t, "left", result.Button)
	assert.Equal(t, 11, result.X)
	assert.Equal(t, 21, result.Y)
}

func TestConvertInputEvent_MouseRelease(t *testing.T) {
	ev := input.MouseReleaseEvent(input.Mouse{
		X:      5,
		Y:      3,
		Button: ansi.MouseNone,
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "mouse", result.Type)
	assert.Equal(t, "release", result.Action)
	assert.Equal(t, "none", result.Button)
}

func TestConvertInputEvent_MouseWheel(t *testing.T) {
	ev := input.MouseWheelEvent(input.Mouse{
		Button: ansi.MouseWheelDown,
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "mouse", result.Type)
	assert.Equal(t, "wheel", result.Action)
	assert.Equal(t, "wheel_down", result.Button)
}

func TestConvertInputEvent_MouseMotion(t *testing.T) {
	ev := input.MouseMotionEvent(input.Mouse{
		X:      1,
		Y:      2,
		Button: ansi.MouseLeft,
		Mod:    input.ModShift,
	})
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "mouse", result.Type)
	assert.Equal(t, "motion", result.Action)
	assert.Equal(t, "left", result.Button)
	assert.Equal(t, 2, result.X)
	assert.Equal(t, 3, result.Y)
	assert.True(t, result.Shift)
}

func TestConvertInputEvent_WindowSize(t *testing.T) {
	ev := input.WindowSizeEvent{Width: 80, Height: 24}
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "resize", result.Type)
	assert.Equal(t, 80, result.Width)
	assert.Equal(t, 24, result.Height)
}

func TestConvertInputEvent_Focus(t *testing.T) {
	focus := ConvertInputEvent(input.FocusEvent{})
	require.NotNil(t, focus)
	assert.Equal(t, "focus", focus.Type)
	assert.True(t, focus.Focused)

	blur := ConvertInputEvent(input.BlurEvent{})
	require.NotNil(t, blur)
	assert.Equal(t, "focus", blur.Type)
	assert.False(t, blur.Focused)
}

func TestConvertInputEvent_Paste(t *testing.T) {
	ev := input.PasteEvent("pasted text")
	result := ConvertInputEvent(ev)
	require.NotNil(t, result)
	assert.Equal(t, "paste", result.Type)
	assert.Equal(t, "pasted text", result.Paste)
}

func TestConvertInputEvent_UnknownReturnsNil(t *testing.T) {
	result := ConvertInputEvent("some unknown event")
	assert.Nil(t, result)
}

func TestConvertInputEvent_MouseButtons(t *testing.T) {
	tests := []struct {
		name   string
		button ansi.MouseButton
	}{
		{name: "none", button: ansi.MouseNone},
		{name: "left", button: ansi.MouseLeft},
		{name: "middle", button: ansi.MouseMiddle},
		{name: "right", button: ansi.MouseRight},
		{name: "wheel_up", button: ansi.MouseWheelUp},
		{name: "wheel_down", button: ansi.MouseWheelDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := input.MouseClickEvent(input.Mouse{Button: tt.button})
			result := ConvertInputEvent(ev)
			require.NotNil(t, result)
			assert.Equal(t, tt.name, result.Button)
		})
	}
}
