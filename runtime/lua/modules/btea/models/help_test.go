package models

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func setupHelpTest(t *testing.T) *engine.VM {
	logger := zap.NewNop()

	// Setup loader with all required dependencies
	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterHelp(L, mod)
		protocol.RegisterKeyBinding(L, mod)
		render.RegisterStyle(L, mod)
		L.Push(mod)
		return 1
	}

	vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
	require.NoError(t, err)
	return vm
}

func TestHelp(t *testing.T) {
	ctx := context.Background()

	// Basic initialization
	t.Run("constructor and default values", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test default constructor
			local help = btea.help({})
			assert(type(help) == "userdata", "help should be userdata")
			
			-- Test with all supported options
			local help2 = btea.help({
				width = 80,
				show_all = false,
				short_separator = " • ",
				full_separator = "    ",
				ellipsis = "...",
				styles = {
					short_key = btea.style():foreground("#909090"),
					short_desc = btea.style():foreground("#B2B2B2"),
					short_separator = btea.style():foreground("#DDDADA"),
					full_key = btea.style():foreground("#909090"),
					full_desc = btea.style():foreground("#B2B2B2"),
					full_separator = btea.style():foreground("#DDDADA"),
					ellipsis = btea.style():foreground("#DDDADA")
				}
			})
			
			local view = help2:view({})
			assert(type(view) == "string", "view should return string")
		`, "test_help_constructor")
		require.NoError(t, err)
	})

	// Display control methods
	t.Run("display control methods", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({
				width = 80,
				show_all = false
			})
			
			-- Test display control methods
			help:set_show_all(true)
			help:set_width(100)
			help:set_separators(" - ", "  ")
			help:set_ellipsis("…")
			
			-- Spawn test keymap
			local keymap = {
				short_help = function()
					return {
						btea.bind({
							keys = {"q"},
							help = {key = "q", desc = "quit"}
						})
					}
				end,
				full_help = function()
					return {
						{
							btea.bind({
								keys = {"q", "ctrl+c"},
								help = {key = "q/ctrl+c", desc = "quit"}
							})
						}
					}
				end
			}

			-- Test view with different settings
			help:set_show_all(true)
			local full_view = help:view(keymap)

			assert(type(full_view) == "string", "view with show_all=true should work")
			assert(#full_view > 0, "full view should not be empty")
			
			help:set_show_all(false)
			local short_view = help:view(keymap)
			assert(type(short_view) == "string", "view with show_all=false should work")
			assert(#short_view > 0, "short view should not be empty")

			-- The views should be different because they show different levels of detail
			assert(full_view ~= short_view, "full and short views should differ")
		`, "test_help_display_control")
		require.NoError(t, err)
	})

	// Style customization
	t.Run("style customization", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({})
			
			-- Test style updates
			help:set_styles({
				short_key = btea.style():foreground("#FF0000"):bold(),
				short_desc = btea.style():foreground("#00FF00"),
				short_separator = btea.style():foreground("#0000FF"),
				full_key = btea.style():foreground("#FFFF00"):bold(),
				full_desc = btea.style():foreground("#00FFFF"),
				full_separator = btea.style():foreground("#FF00FF"),
				ellipsis = btea.style():foreground("#FFFFFF")
			})
			
			-- Test that styles are applied
			local keymap = {
				short_help = {
					btea.bind({
						keys = {"q"},
						help = {key = "q", desc = "quit"}
					})
				}
			}
			
			local view = help:view(keymap)
			assert(type(view) == "string", "view with styles should work")
		`, "test_help_styling")
		require.NoError(t, err)
	})

	// KeyMap interface implementations
	t.Run("keymap interface - table implementation", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({})
			
			-- Test with table-based keymap
			local bindings = {
				quit = btea.bind({
					keys = {"q", "ctrl+c"},
					help = {key = "q/ctrl+c", desc = "quit"}
				}),
				help = btea.bind({
					keys = {"?"},
					help = {key = "?", desc = "toggle help"}
				}),
				save = btea.bind({
					keys = {"ctrl+s"},
					help = {key = "ctrl+s", desc = "save"}
				})
			}

			local keymap = {
				short_help = {
					bindings.quit,
					bindings.help
				},
				
				full_help = {
					{  -- File operations
						bindings.save,
						bindings.quit
					},
					{  -- Help
						bindings.help
					}
				}
			}
			
			-- Test help views
			local short_help = help:get_short_help(keymap)
			assert(#short_help == 2, "should have 2 short help bindings")
			
			local full_help = help:get_full_help(keymap)
			assert(#full_help == 2, "should have 2 groups of bindings")
			
			local view = help:view(keymap)
			assert(type(view) == "string", "view should work with table keymap")
		`, "test_help_keymap_table")
		require.NoError(t, err)
	})

	// Direct bindings (without functions)
	t.Run("keymap interface - direct bindings", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({})
			
			-- Test with direct bindings
			local keymap = {
				short_help = {
					btea.bind({
						keys = {"q"},
						help = {key = "q", desc = "quit"}
					}),
					btea.bind({
						keys = {"?"},
						help = {key = "?", desc = "help"}
					})
				},
				full_help = {
					{  -- First group
						btea.bind({
							keys = {"q"},
							help = {key = "q", desc = "quit"}
						})
					},
					{  -- Second group
						btea.bind({
							keys = {"?"},
							help = {key = "?", desc = "help"}
						})
					}
				}
			}
			
			-- Test help views
			local short_help = help:get_short_help(keymap)
			assert(#short_help == 2, "should have 2 short help bindings")
			
			local full_help = help:get_full_help(keymap)
			assert(#full_help == 2, "should have 2 groups of bindings")
			
			local view = help:view(keymap)
			assert(type(view) == "string", "view should work with direct bindings")
		`, "test_help_direct_bindings")
		require.NoError(t, err)
	})

	// Message handling
	t.Run("message handling", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({})
			
			-- Test window resize message
			local resize_msg = {
				type = "window_resize",
				width = 100,
				height = 50
			}
			local cmd = help:update(resize_msg)
			assert(cmd == nil, "resize update should return nil")
			
			-- Test key message
			local key_msg = {
				type = "key",
				key_type = "char",
				char = "?",
				runes = {"?"}
			}
			cmd = help:update(key_msg)
			assert(cmd == nil, "key update should return nil")
		`, "test_help_messages")
		require.NoError(t, err)
	})

	// Combined usage
	t.Run("combined component usage", func(t *testing.T) {
		vm := setupHelpTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local btea = require("btea")
			
			local help = btea.help({})
			
			-- Spawn multiple keymaps
			local keymap1 = {
				short_help = {
					btea.bind({
						keys = {"q"},
						help = {key = "q", desc = "quit"}
					})
				}
			}
			
			local keymap2 = {
				short_help = {
					btea.bind({
						keys = {"?"},
						help = {key = "?", desc = "help"}
					})
				}
			}
			
			-- Test combined keymap
			local combined = {
				short_help = function()
					-- Combine bindings from both keymaps
					local help1 = help:get_short_help(keymap1)
					local help2 = help:get_short_help(keymap2)
					local combined = {}
					for _, v in ipairs(help1) do table.insert(combined, v) end
					for _, v in ipairs(help2) do table.insert(combined, v) end
					return combined
				end,
				
				full_help = function()
					return {
						help:get_short_help(keymap1),
						help:get_short_help(keymap2)
					}
				end
			}
			
			-- Test combined view
			local view = help:view(combined)
			assert(type(view) == "string", "combined view should work")
		`, "test_help_combined")
		require.NoError(t, err)
	})
}
