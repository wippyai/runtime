package protocol

import (
	"testing"

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

		err = vm.DoString(nil, `
            -- Create a key binding
            local binding = btea.new_binding({
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

		err = vm.DoString(nil, `
            -- Create a binding for space key
            local binding = btea.new_binding({
                keys = {"space"},
                help = {key = "space", desc = "select"}
            })

            -- Create a key message (this would come from the TUI normally)
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

		err = vm.DoString(nil, `
            -- Test empty keys
            local binding = btea.new_binding({
                keys = {} -- empty but valid keys table
            })
            assert(binding:is_enabled(), "binding should be enabled with empty keys")

            -- Test with multiple keys
            local multi = btea.new_binding({
                keys = {"ctrl+x", "ctrl+w", "q"}
            })
            assert(multi:is_enabled(), "multi-key binding should work")

            -- Test with just help
            local help_only = btea.new_binding({
                help = {key = "test", desc = "test binding"}
            })
            local help = help_only:help()
            assert(help.key == "test", "help key should be set")
            assert(help.desc == "test binding", "help desc should be set")
        `, "test_special")
		require.NoError(t, err)
	})
}
