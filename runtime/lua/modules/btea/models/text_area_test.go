package models

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestTextArea(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterTextArea(L, mod)
		protocol.RegisterKeyBinding(L, mod) // Register key bindings
		render.RegisterStyle(L, mod)        // Register styles
		L.Push(mod)
		return 1
	}

	t.Run("text area creation and basic configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			-- Test default constructor
			local ta1 = btea.text_area({
				width = 40,
				height = 10
			})
			assert(type(ta1) == "userdata", "text area should be userdata")
			
			-- Test with custom options
			local ta2 = btea.text_area({
				prompt = "> ",
				placeholder = "Type something...",
				value = "Initial text",
				width = 60,
				height = 20,
				char_limit = 100,
				show_line_numbers = true
			})
			
			-- Test content operations
			local view = ta2:view()
			assert(type(view) == "string", "view should return a string")
			assert(ta2:value() == "Initial text", "value should match initial text")
			
			-- Test content modification
			ta2:set_value("Updated text")
			assert(ta2:value() == "Updated text", "value should be updated")
			
			-- Test dimensions
			assert(ta2:line_count() > 0, "line count should be positive")
			assert(ta2:length() > 0, "length should be positive")
		`, "test_text_area_basic")

		require.NoError(t, err)
	})

	t.Run("text area focus and blur", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local ta = btea.text_area({
				width = 40,
				height = 10
			})
			
			-- Test focus
			local cmd = ta:focus()
			assert(cmd ~= nil, "focus should return a command")
			
			-- Test blur
			ta:blur()
		`, "test_text_area_focus")

		require.NoError(t, err)
	})

	t.Run("text area styling", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			-- Create text area with custom styles
			local ta = btea.text_area({
				width = 40,
				height = 10,
				focused_style = {
					base = btea.style():foreground("#FFFFFF"),
					text = btea.style():foreground("#00FF00"),
					prompt = btea.style():foreground("#FFFF00"),
					cursor_line = btea.style():background("#333333"),
					cursor_line_number = btea.style():foreground("#FF0000"),
					line_number = btea.style():foreground("#666666"),
					placeholder = btea.style():foreground("#888888")
				},
				blurred_style = {
					base = btea.style():foreground("#CCCCCC"),
					text = btea.style():foreground("#888888")
				}
			})
			
			-- Verify styles are applied
			local view = ta:view()
			assert(type(view) == "string", "view with styles should return string")
		`, "test_text_area_styling")

		require.NoError(t, err)
	})

	t.Run("text area key bindings", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			-- Create bindings helper function
			local function bind(keys, help_key, help_desc)
				return btea.bind({
					keys = keys,
					help = {key = help_key, desc = help_desc}
				})
			end
			
			-- Create text area with custom key bindings
			local ta = btea.text_area({
				width = 40,
				height = 10,
				key_map = {
					character_forward = bind(
						{"right", "ctrl+f"},
						"→/^F",
						"move forward"
					),
					character_backward = bind(
						{"left", "ctrl+b"},
						"←/^B",
						"move backward"
					),
					insert_newline = bind(
						{"enter"},
						"⏎",
						"new line"
					)
				}
			})
			
			-- Test key handling
			ta:focus()
			ta:set_value("test")
			local msg = {
				type = "key",
				key_type = "right"
			}
			local cmd = ta:update(msg)
		`, "test_text_area_bindings")

		require.NoError(t, err)
	})
}

func TestTextAreaUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the text area module
	mod := cvm.State().NewTable()
	RegisterTextArea(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	err = cvm.StartString(context.Background(), `
		local text_area = btea.text_area({
			width = 40,
			height = 10,
			placeholder = "Type something..."
		})

		-- Focus and get initial command
		local cmd = text_area:focus()
		assert(cmd ~= nil, "focus should return a command")
		
		-- Create initial view
		local initial_view = text_area:view()
		
		-- Yield for first input message
		local msg = coroutine.yield("ready_for_input", cmd)
		
		-- Process typing
		cmd = text_area:update(msg)
		assert(text_area:value() ~= "", "value should be updated after typing")
		
		-- Process newline
		msg = coroutine.yield("ready_for_newline", cmd)
		cmd = text_area:update(msg)
		assert(text_area:line_count() > 1, "line count should increase after newline")
		
		-- Final state
		assert(text_area:length() > 0, "text area should contain content")
		
		coroutine.yield("done", cmd)
	`, "test_text_area_update")
	require.NoError(t, err)

	// First yield - after focus
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, "ready_for_input", tasks[0].Yielded[0].String())

	// Simulate typing "Hello"
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Hello")})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "ready_for_newline", tasks[0].Yielded[0].String())

	// Simulate pressing enter
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyEnter})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}
