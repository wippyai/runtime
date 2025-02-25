package models

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/ponyruntime/pony/runtime/uow"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestTextInput(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterTextInput(L, mod)
		protocol.RegisterKeyBinding(L, mod) // Register key bindings
		render.RegisterStyle(L, mod)        // Register styles
		render.RegisterStyle(L, mod)        // Register styles
		L.Push(mod)
		return 1
	}

	ctx, uw := uow.OnContext(context.Background())
	defer func() { _ = uw.Close() }()

	t.Run("text input creation and basic configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test default constructor
			local input1 = btea.text_input({})
			assert(type(input1) == "userdata", "text input should be userdata")
			
			-- Test with placeholder and prompt
			local input2 = btea.text_input({
				placeholder = "Enter text...",
				prompt = "> "
			})
			local view = input2:view()
			assert(type(view) == "string", "view should return a string")
			
			-- Test with initial value and width
			local input3 = btea.text_input({
				value = "initial text",
				width = 40
			})
			assert(input3:value() == "initial text", "value should match initial text")
			
			-- Test with character limit
			local input4 = btea.text_input({
				char_limit = 10,
				value = "1234567890"
			})
			input4:set_value(input4:value() .. "overflow")
			assert(#input4:value() == 10, "value should be limited to 10 characters")
		`, "test_text_input_basic")

		require.NoError(t, err)
	})

	t.Run("text input styling", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
				
			-- Test with styles
			local input = btea.text_input({
				prompt_style = btea.style():foreground("#00FF00"):bold(),
				text_style = btea.style():foreground("#FFFFFF"),
				placeholder_style = btea.style():foreground("#666666"),
				completion_style = btea.style():foreground("#888888"),
				cursor_style = btea.style():foreground("#FFFF00")
			})
			
			-- Test style updates
			input:set_style("prompt", btea.style():foreground("#FF0000"))
			input:set_style("text", btea.style():bold())
			input:set_style("placeholder", btea.style():italic())
		`, "test_text_input_styling")

		require.NoError(t, err)
	})

	t.Run("text input echo modes", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test normal mode
			local input1 = btea.text_input({
				echo_mode = btea.ECHO_NORMAL,
				value = "visible"
			})
			assert(input1:view():find("visible"), "text should be visible in normal mode")
			
			-- Test password mode
			local input2 = btea.text_input({
				echo_mode = btea.ECHO_PASSWORD,
				echo_character = "*",
				value = "secret"
			})
			local view = input2:view()
			assert(not view:find("secret"), "text should not be visible in password mode")
			assert(view:find("******"), "should show asterisks in password mode")
			
			-- Test none mode
			local input3 = btea.text_input({
				echo_mode = btea.ECHO_NONE,
				value = "hidden"
			})
			assert(not input3:view():find("hidden"), "text should not be visible in none mode")
		`, "test_text_input_echo_modes")

		require.NoError(t, err)
	})

	t.Run("text input validation", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test length validation
			local input1 = btea.text_input({
				validate = function(text)
					if #text < 3 then
						return "Text must be at least 3 characters"
					end
					return nil
				end
			})
			
			input1:set_value("ab")
			assert(not input1:is_valid(), "short input should be invalid")
			assert(input1:error() == "Text must be at least 3 characters", "should show correct error")
			
			input1:set_value("abc")
			assert(input1:is_valid(), "valid input should pass validation")
			assert(input1:error() == nil, "no error for valid input")
			
			-- Test dynamic validation update
			input1:set_validate(function(text)
				if #text > 5 then
					return "Text too long"
				end
				return nil
			end)
			
			input1:set_value("toolong")
			assert(not input1:is_valid(), "long input should be invalid")
			assert(input1:error() == "Text too long", "should show updated error")
		`, "test_text_input_validation")

		require.NoError(t, err)
	})

	t.Run("text input suggestions", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			local input = btea.text_input({
				show_suggestions = true,
				suggestions = {"help", "status", "quit", "clear"}
			})
			
			-- Test initial suggestions
			local suggestions = input:get_suggestions()
			assert(#suggestions == 4, "should have all initial suggestions")
			
			-- Test updating suggestions
			input:set_suggestions({"new", "commands"})
			suggestions = input:get_suggestions()
			assert(#suggestions == 2, "should have updated suggestions")
			assert(suggestions[1] == "new", "should have new suggestions")
		`, "test_text_input_suggestions")

		require.NoError(t, err)
	})

	t.Run("text input key bindings", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Spawn bindings helper function
			local function bind(keys, help_key, help_desc)
				return btea.bind({
					keys = keys,
					help = {key = help_key, desc = help_desc}
				})
			end
			
			-- Spawn custom key bindings
			local input = btea.text_input({
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
					word_forward = bind(
						{"alt+right", "alt+f"},
						"M-→/M-F",
						"word forward"
					),
					word_backward = bind(
						{"alt+left", "alt+b"},
						"M-←/M-B",
						"word backward"
					),
					delete_character_backward = bind(
						{"backspace", "ctrl+h"},
						"⌫/^H",
						"delete backward"
					),
					accept_suggestion = bind(
						{"tab"},
						"⇥",
						"complete"
					)
				}
			})
			
			-- Set some text and test cursor movement
			input:set_value("test text")
			input:cursor_start()
			input:focus()
			assert(input:position() == 0, "cursor should be at start")
			
			-- Simulate right arrow key
			local msg = {
				type = "update",
				key = {
					type = "key",
					key_type = "right"
				}
			}
			input:update(msg)
			assert(input:position() == 1, "cursor should move right")
		`, "test_text_input_binds")

		require.NoError(t, err)
	})
}

func TestTextInputUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the text input module
	mod := cvm.State().NewTable()
	RegisterTextInput(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	ctx, uw := uow.OnContext(context.Background())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
		local input = btea.text_input({
			prompt = "> ",
			placeholder = "Type something..."
		})

		-- Focus the input and store initial state
		local cmd = input:focus()
		assert(cmd ~= nil, "focus should return a command")
		local initial_view = input:view()
		
		-- Yield for first input message
		local msg = coroutine.yield("ready_for_input", cmd)
		
		-- Process typing 'hello'
		cmd = input:update(msg)
		assert(input:value() == "h", "first character should be entered")
		
		-- Simulate more typing
		msg = coroutine.yield("typing", cmd)
		cmd = input:update(msg)
		assert(input:value() == "he", "second character should be entered")
		
		-- Test cursor movement
		input:cursor_start()
		assert(input:position() == 0, "cursor should be at start")
		
		input:cursor_end()
		assert(input:position() == 2, "cursor should be at end")
		
		-- Test character deletion
		msg = coroutine.yield("deleting", cmd)
		cmd = input:update(msg)
		assert(input:value() == "h", "character should be deleted")
		
		coroutine.yield("done", cmd)
	`, "test_text_input_update")
	require.NoError(t, err)

	// First yield - after focus
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, "ready_for_input", tasks[0].Yielded[0].String())

	// Simulate typing 'h'
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "typing", tasks[0].Yielded[0].String())

	// Simulate typing 'e'
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "deleting", tasks[0].Yielded[0].String())

	// Simulate backspace
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyBackspace})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}
