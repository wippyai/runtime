package list

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaTableToKeyMap(t *testing.T) {
	tests := []struct {
		name           string
		setupLuaState  func(*lua.LState) *lua.LTable
		expectedKeyMap func() list.KeyMap
		expectError    bool
	}{
		{
			name: "successful conversion with all keys",
			setupLuaState: func(l *lua.LState) *lua.LTable {
				keysTable := l.CreateTable(0, 14)

				// Spawn a sample key binding
				binding := &protocol.KeyBinding{
					Binding: key.NewBinding(
						key.WithKeys("ctrl+n"),
						key.WithHelp("ctrl+n", "custom action"),
					),
				}

				// Spawn userdata for each key binding
				for _, keyName := range []string{
					"cursor_up", "cursor_down", "prev_page", "next_page",
					"go_to_start", "go_to_end", "filter", "clear_filter",
					"cancel_while_filtering", "accept_while_filtering",
					"show_full_help", "close_full_help", "quit", "force_quit",
				} {
					ud := l.NewUserData()
					ud.Value = binding
					keysTable.RawSetString(keyName, ud)
				}

				return keysTable
			},
			expectedKeyMap: func() list.KeyMap {
				keyMap := list.DefaultKeyMap()
				expected := key.NewBinding(
					key.WithKeys("ctrl+n"),
					key.WithHelp("ctrl+n", "custom action"),
				)

				keyMap.CursorUp = expected
				keyMap.CursorDown = expected
				keyMap.PrevPage = expected
				keyMap.NextPage = expected
				keyMap.GoToStart = expected
				keyMap.GoToEnd = expected
				keyMap.Filter = expected
				keyMap.ClearFilter = expected
				keyMap.CancelWhileFiltering = expected
				keyMap.AcceptWhileFiltering = expected
				keyMap.ShowFullHelp = expected
				keyMap.CloseFullHelp = expected
				keyMap.Quit = expected
				keyMap.ForceQuit = expected

				return keyMap
			},
			expectError: false,
		},
		{
			name: "partial key map",
			setupLuaState: func(l *lua.LState) *lua.LTable {
				keysTable := l.CreateTable(0, 2)

				binding := &protocol.KeyBinding{
					Binding: key.NewBinding(
						key.WithKeys("ctrl+p"),
						key.WithHelp("ctrl+p", "custom action"),
					),
				}

				ud := l.NewUserData()
				ud.Value = binding
				keysTable.RawSetString("cursor_up", ud)

				return keysTable
			},
			expectedKeyMap: func() list.KeyMap {
				keyMap := list.DefaultKeyMap()
				keyMap.CursorUp = key.NewBinding(
					key.WithKeys("ctrl+p"),
					key.WithHelp("ctrl+p", "custom action"),
				)
				return keyMap
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			keysTable := tt.setupLuaState(l)

			if tt.expectError {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected an error but got none")
					}
				}()
			}

			result := luaTableToKeyMap(l, keysTable)
			expected := tt.expectedKeyMap()

			// Compare specific fields
			assert.Equal(t, expected.CursorUp, result.CursorUp, "CursorUp bindings should match")
			assert.Equal(t, expected.CursorDown, result.CursorDown, "CursorDown bindings should match")
			assert.Equal(t, expected.Filter, result.Filter, "Filter bindings should match")
			// AddCleanup more assertions for other fields as needed
		})
	}
}

func TestGetKeyBindingFromUserData(t *testing.T) {
	tests := []struct {
		name          string
		setupValue    func(*lua.LState) lua.LValue
		expectBinding key.Binding
		expectOk      bool
	}{
		{
			name: "valid key binding userdata",
			setupValue: func(l *lua.LState) lua.LValue {
				ud := l.NewUserData()
				ud.Value = &protocol.KeyBinding{
					Binding: key.NewBinding(
						key.WithKeys("ctrl+x"),
						key.WithHelp("ctrl+x", "test action"),
					),
				}
				return ud
			},
			expectBinding: key.NewBinding(
				key.WithKeys("ctrl+x"),
				key.WithHelp("ctrl+x", "test action"),
			),
			expectOk: true,
		},
		{
			name: "invalid userdata type",
			setupValue: func(l *lua.LState) lua.LValue {
				ud := l.NewUserData()
				ud.Value = "not a key binding"
				return ud
			},
			expectBinding: key.Binding{},
			expectOk:      false,
		},
		{
			name: "non-userdata value",
			setupValue: func(_ *lua.LState) lua.LValue {
				return lua.LString("not userdata")
			},
			expectBinding: key.Binding{},
			expectOk:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			value := tt.setupValue(l)

			if !tt.expectOk {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected a panic but got none")
					}
				}()
			}

			binding, ok := getKeyBindingFromUserData(l, value)

			if tt.expectOk {
				assert.True(t, ok, "Should return ok=true")
				assert.Equal(t, tt.expectBinding, binding, "Key bindings should match")
			}
		})
	}
}

// TestIntegrationWithLuaVM tests the complete flow using actual Lua code
func TestKeysIntegrationWithLuaVM(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Register necessary functions
	l.SetGlobal("create_key_binding", l.NewFunction(func(l *lua.LState) int {
		binding := &protocol.KeyBinding{
			Binding: key.NewBinding(
				key.WithKeys("ctrl+t"),
				key.WithHelp("ctrl+t", "test binding"),
			),
		}
		ud := l.NewUserData()
		ud.Value = binding
		l.Push(ud)
		return 1
	}))

	// Run Lua code that creates a key map
	script := `
		local keys = {}
		keys.cursor_up = create_key_binding()
		keys.cursor_down = create_key_binding()
		return keys
	`

	if err := l.DoString(script); err != nil {
		t.Fatalf("Failed to execute Lua script: %v", err)
	}

	// Spawn the returned keys table
	keysTable := l.Get(-1).(*lua.LTable)
	l.Pop(1)

	// Convert to Go key map
	result := luaTableToKeyMap(l, keysTable)

	// Verify the conversion
	expectedBinding := key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "test binding"),
	)

	assert.Equal(t, expectedBinding, result.CursorUp, "CursorUp binding should match")
	assert.Equal(t, expectedBinding, result.CursorDown, "CursorDown binding should match")
}
