package protocol

import (
	"github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
	"reflect"
	"testing"
)

func TestKeyMessageConversion(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{
			name: "simple runes",
			msg: tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune("a"),
			},
		},
		{
			name: "enter key",
			msg: tea.KeyMsg{
				Type: tea.KeyEnter,
			},
		},
		{
			name: "alt+runes",
			msg: tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune("x"),
				Alt:   true,
			},
		},
		{
			name: "paste mode",
			msg: tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune("v"),
				Paste: true,
			},
		},
		{
			name: "function key",
			msg: tea.KeyMsg{
				Type: tea.KeyF1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to Lua
			luaVal := MsgToLua(tt.msg)
			if luaVal.Type() != lua.LTTable {
				t.Fatalf("expected lua table, got %v", luaVal.Type())
			}

			// Convert back to KeyMsg
			msg, err := LuaToMsg(luaVal)
			if err != nil {
				t.Fatalf("error converting back: %v", err)
			}

			// Compare
			keyMsg, ok := msg.(tea.KeyMsg)
			if !ok {
				t.Fatalf("expected KeyMsg, got %T", msg)
			}

			if keyMsg.Type != tt.msg.Type {
				t.Errorf("key type mismatch: got %v, want %v", keyMsg.Type, tt.msg.Type)
			}
			if keyMsg.Alt != tt.msg.Alt {
				t.Errorf("alt mismatch: got %v, want %v", keyMsg.Alt, tt.msg.Alt)
			}
			if keyMsg.Paste != tt.msg.Paste {
				t.Errorf("paste mismatch: got %v, want %v", keyMsg.Paste, tt.msg.Paste)
			}
			if tt.msg.Type == tea.KeyRunes && string(keyMsg.Runes) != string(tt.msg.Runes) {
				t.Errorf("runes mismatch: got %v, want %v", string(keyMsg.Runes), string(tt.msg.Runes))
			}
		})
	}
}

func TestMouseMessageConversion(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.MouseMsg
	}{
		{
			name: "left click",
			msg: tea.MouseMsg{
				X:      10,
				Y:      20,
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
			},
		},
		{
			name: "right click with modifiers",
			msg: tea.MouseMsg{
				X:      30,
				Y:      40,
				Button: tea.MouseButtonRight,
				Action: tea.MouseActionPress,
				Alt:    true,
				Ctrl:   true,
				Shift:  true,
			},
		},
		{
			name: "mouse motion",
			msg: tea.MouseMsg{
				X:      50,
				Y:      60,
				Button: tea.MouseButtonNone,
				Action: tea.MouseActionMotion,
			},
		},
		{
			name: "scroll wheel",
			msg: tea.MouseMsg{
				X:      70,
				Y:      80,
				Button: tea.MouseButtonWheelUp,
				Action: tea.MouseActionPress,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to Lua
			luaVal := MsgToLua(tt.msg)
			if luaVal.Type() != lua.LTTable {
				t.Fatalf("expected lua table, got %v", luaVal.Type())
			}

			// Convert back to MouseMsg
			msg, err := LuaToMsg(luaVal)
			if err != nil {
				t.Fatalf("error converting back: %v", err)
			}

			// Compare
			mouseMsg, ok := msg.(tea.MouseMsg)
			if !ok {
				t.Fatalf("expected MouseMsg, got %T", msg)
			}

			if mouseMsg.X != tt.msg.X || mouseMsg.Y != tt.msg.Y {
				t.Errorf("position mismatch: got (%v,%v), want (%v,%v)",
					mouseMsg.X, mouseMsg.Y, tt.msg.X, tt.msg.Y)
			}
			if mouseMsg.Button != tt.msg.Button {
				t.Errorf("button mismatch: got %v, want %v", mouseMsg.Button, tt.msg.Button)
			}
			if mouseMsg.Action != tt.msg.Action {
				t.Errorf("action mismatch: got %v, want %v", mouseMsg.Action, tt.msg.Action)
			}
			if mouseMsg.Alt != tt.msg.Alt {
				t.Errorf("alt mismatch: got %v, want %v", mouseMsg.Alt, tt.msg.Alt)
			}
			if mouseMsg.Ctrl != tt.msg.Ctrl {
				t.Errorf("ctrl mismatch: got %v, want %v", mouseMsg.Ctrl, tt.msg.Ctrl)
			}
			if mouseMsg.Shift != tt.msg.Shift {
				t.Errorf("shift mismatch: got %v, want %v", mouseMsg.Shift, tt.msg.Shift)
			}
		})
	}
}

func TestWindowSizeMessageConversion(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.WindowSizeMsg
	}{
		{
			name: "standard size",
			msg: tea.WindowSizeMsg{
				Width:  80,
				Height: 24,
			},
		},
		{
			name: "large window",
			msg: tea.WindowSizeMsg{
				Width:  1920,
				Height: 1080,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to Lua
			luaVal := MsgToLua(tt.msg)
			if luaVal.Type() != lua.LTTable {
				t.Fatalf("expected lua table, got %v", luaVal.Type())
			}

			// Convert back to WindowSizeMsg
			msg, err := LuaToMsg(luaVal)
			if err != nil {
				t.Fatalf("error converting back: %v", err)
			}

			// Compare
			sizeMsg, ok := msg.(tea.WindowSizeMsg)
			if !ok {
				t.Fatalf("expected WindowSizeMsg, got %T", msg)
			}

			if sizeMsg.Width != tt.msg.Width || sizeMsg.Height != tt.msg.Height {
				t.Errorf("size mismatch: got %vx%v, want %vx%v",
					sizeMsg.Width, sizeMsg.Height, tt.msg.Width, tt.msg.Height)
			}
		})
	}
}

func TestOpaqueMessageHandling(t *testing.T) {
	type customMsg struct {
		value string
	}

	msg := customMsg{value: "test"}

	// Convert to Lua
	luaVal := MsgToLua(msg)
	if luaVal.Type() != lua.LTTable {
		t.Fatalf("expected lua table, got %v", luaVal.Type())
	}

	// Verify opaque field exists
	tbl := luaVal.(*lua.LTable)
	if opaque := tbl.RawGetString("opaque"); opaque == lua.LNil {
		t.Fatal("missing opaque field")
	}

	// Convert back
	result, err := LuaToMsg(luaVal)
	if err != nil {
		t.Fatalf("error converting back: %v", err)
	}

	// Compare
	converted, ok := result.(customMsg)
	if !ok {
		t.Fatalf("expected customMsg, got %T", result)
	}

	if !reflect.DeepEqual(converted, msg) {
		t.Errorf("value mismatch: got %v, want %v", converted, msg)
	}
}

func TestInitialization(t *testing.T) {
	t.Run("key type mappings", func(t *testing.T) {
		if len(keyTypeMap) == 0 {
			t.Error("keyTypeMap not initialized")
		}
		if len(keyTypeFromStr) == 0 {
			t.Error("keyTypeFromStr not initialized")
		}

		// Test bidirectional mapping
		for keyType, str := range keyTypeMap {
			reverseKeyType, ok := keyTypeFromStr[str]
			if !ok {
				t.Errorf("missing reverse mapping for %v", str)
			}
			if keyType != reverseKeyType {
				t.Errorf("mapping mismatch: %v != %v", keyType, reverseKeyType)
			}
		}
	})

	t.Run("mouse button mappings", func(t *testing.T) {
		if len(mouseButtonMap) == 0 {
			t.Error("mouseButtonMap not initialized")
		}
		if len(mouseButtonFromStr) == 0 {
			t.Error("mouseButtonFromStr not initialized")
		}

		// Test bidirectional mapping
		for button, str := range mouseButtonMap {
			reverseButton, ok := mouseButtonFromStr[str]
			if !ok {
				t.Errorf("missing reverse mapping for %v", str)
			}
			if button != reverseButton {
				t.Errorf("mapping mismatch: %v != %v", button, reverseButton)
			}
		}
	})
}
