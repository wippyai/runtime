package protocol

import (
	"context"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestKeyBinding(t *testing.T) {
	logger := zap.NewNop()

	t.Run("key binding basic functionality", func(t *testing.T) {
		vm, err := engine.NewVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register our module
		mod := vm.State().NewTable()
		RegisterKeyBinding(vm.State(), mod)
		vm.State().SetGlobal("btea", mod)

		err = vm.DoString(context.Background(), `
            -- Spawn a key binding
            local binding = btea.bind({
                keys = {"ctrl+c", "esc"},
                help = {
                    key = "ctrl+c/esc",
                    desc = "quit"
                }
            })

            -- Test methods
            assert(binding:is_enabled() == true, "binding should be enabled by default")
            binding:set_enabled(false)
            assert(binding:is_enabled() == false, "binding should be disabled")

            -- Test help text
            local help = binding:help()
            assert(help.key == "ctrl+c/esc", "unexpected help key")
            assert(help.desc == "quit", "unexpected help description")
        `, "test_basic")
		require.NoError(t, err)
	})

	t.Run("key matching", func(t *testing.T) {
		vm, err := engine.NewVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		mod := vm.State().NewTable()
		RegisterKeyBinding(vm.State(), mod)
		vm.State().SetGlobal("btea", mod)

		err = vm.DoString(context.Background(), `
            -- Spawn a binding for space key
            local binding = btea.bind({
                keys = {"space"},
                help = {key = "space", desc = "select"}
            })

            -- Spawn a key message (this would come from the TUI normally)
            local msg = {
                type = "update",
                key = {
                    type = "key",
                    key_type = "space",
                    string = " ",
                    alt = false,
                    paste = false
                }
            }

            -- Test matching
            assert(binding:matches(msg) == true, "binding should match space key")

            -- Test non-matching key
            local non_match = {
                type = "update",
                key = {
                    type = "key",
                    key_type = "enter",
                    string = "",
                    alt = false,
                    paste = false
                }
            }
            assert(binding:matches(non_match) == false, "binding should not match enter key")
        `, "test_matching")
		require.NoError(t, err)
	})

	t.Run("special cases", func(t *testing.T) {
		vm, err := engine.NewVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		mod := vm.State().NewTable()
		RegisterKeyBinding(vm.State(), mod)
		vm.State().SetGlobal("btea", mod)

		err = vm.DoString(context.Background(), `
            -- Test empty keys
            local binding = btea.bind({
                keys = {} -- empty but valid keys table
            })
            assert(binding:is_enabled(), "binding should be enabled with empty keys")

            -- Test with multiple keys
            local multi = btea.bind({
                keys = {"ctrl+x", "ctrl+w", "q"}
            })
            assert(multi:is_enabled(), "multi-key binding should work")

            -- Test with just help
            local help_only = btea.bind({
                help = {key = "test", desc = "test binding"}
            })
            local help = help_only:help()
            assert(help.key == "test", "help key should be set")
            assert(help.desc == "test binding", "help desc should be set")
        `, "test_special")
		require.NoError(t, err)
	})
}

func TestKeyBindingConversion(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Register the binding type for proper metatable setup
	mod := l.NewTable()
	RegisterKeyBinding(l, mod)

	t.Run("ToLuaKeyBinding", func(t *testing.T) {
		// Test conversion of a simple binding
		binding := key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("^C", "quit"),
		)

		luaValue := ToLuaKeyBinding(l, binding)
		require.NotNil(t, luaValue)

		// Check type and value
		ud, ok := luaValue.(*lua.LUserData)
		require.True(t, ok, "expected userdata")

		kb, ok := ud.Value.(*KeyBinding)
		require.True(t, ok, "expected KeyBinding")

		// Verify metatable is set
		mt := l.GetMetatable(luaValue)
		require.NotNil(t, mt, "metatable should be set")

		// Verify binding properties
		help := kb.Binding.Help()
		assert.Equal(t, "^C", help.Key)
		assert.Equal(t, "quit", help.Desc)
	})

	t.Run("ToGoKeyBinding success", func(t *testing.T) {
		// Spawn a KeyBinding userdata
		kb := &KeyBinding{
			Binding: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("↵", "submit"),
			),
		}
		ud := l.NewUserData()
		ud.Value = kb

		// Convert to Go binding
		goBinding, ok := ToGoKeyBinding(ud)
		require.True(t, ok, "conversion should succeed")

		// Verify properties
		help := goBinding.Help()
		assert.Equal(t, "↵", help.Key)
		assert.Equal(t, "submit", help.Desc)
	})

	t.Run("ToGoKeyBinding failure cases", func(t *testing.T) {
		// Test nil
		binding, ok := ToGoKeyBinding(lua.LNil)
		assert.False(t, ok, "nil should fail conversion")
		assert.Equal(t, key.Binding{}, binding)

		// Test wrong type of userdata
		ud := l.NewUserData()
		ud.Value = "not a binding"
		binding, ok = ToGoKeyBinding(ud)
		assert.False(t, ok, "wrong userdata type should fail conversion")
		assert.Equal(t, key.Binding{}, binding)

		// Test regular table
		binding, ok = ToGoKeyBinding(l.NewTable())
		assert.False(t, ok, "table should fail conversion")
		assert.Equal(t, key.Binding{}, binding)

		// Test number
		binding, ok = ToGoKeyBinding(lua.LNumber(42))
		assert.False(t, ok, "number should fail conversion")
		assert.Equal(t, key.Binding{}, binding)
	})

	t.Run("roundtrip conversion", func(t *testing.T) {
		// Launch with a Go binding
		original := key.NewBinding(
			key.WithKeys("ctrl+x", "cmd+x"),
			key.WithHelp("^X", "cut"),
		)

		// Convert to Lua
		luaValue := ToLuaKeyBinding(l, original)
		require.NotNil(t, luaValue)

		// Convert back to Go
		converted, ok := ToGoKeyBinding(luaValue)
		require.True(t, ok, "roundtrip conversion should succeed")

		// Compare properties
		originalHelp := original.Help()
		convertedHelp := converted.Help()
		assert.Equal(t, originalHelp.Key, convertedHelp.Key)
		assert.Equal(t, originalHelp.Desc, convertedHelp.Desc)
	})
}
