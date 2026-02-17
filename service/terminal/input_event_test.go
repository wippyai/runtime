package terminal

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/input"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		code    rune
		keyType string
	}{
		{"enter", input.KeyEnter, "enter"},
		{"tab", input.KeyTab, "tab"},
		{"escape", input.KeyEscape, "esc"},
		{"backspace", input.KeyBackspace, "backspace"},
		{"up", input.KeyUp, "up"},
		{"down", input.KeyDown, "down"},
		{"left", input.KeyLeft, "left"},
		{"right", input.KeyRight, "right"},
		{"home", input.KeyHome, "home"},
		{"end", input.KeyEnd, "end"},
		{"pgup", input.KeyPgUp, "pgup"},
		{"pgdown", input.KeyPgDown, "pgdown"},
		{"delete", input.KeyDelete, "delete"},
		{"insert", input.KeyInsert, "insert"},
		{"f1", input.KeyF1, "f1"},
		{"f12", input.KeyF12, "f12"},
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
	assert.Equal(t, 10, result.X)
	assert.Equal(t, 20, result.Y)
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
		button ansi.MouseButton
		name   string
	}{
		{ansi.MouseNone, "none"},
		{ansi.MouseLeft, "left"},
		{ansi.MouseMiddle, "middle"},
		{ansi.MouseRight, "right"},
		{ansi.MouseWheelUp, "wheel_up"},
		{ansi.MouseWheelDown, "wheel_down"},
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
