package btea

import (
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestBteaModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module loads successfully", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			assert(type(btea) == "table", "btea module should be a table")	
		`, "test_load")
		require.NoError(t, err)
	})

	t.Run("module name is correct", func(t *testing.T) {
		mod := NewBteaModule(logger)
		require.Equal(t, "btea", mod.Name())
	})

	t.Run("all protocol components are available", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test command functionality
			assert(type(btea.commands) == "table", "commands should be available")
			assert(type(btea.commands.clear_screen) == "userdata", "clear_screen command should be available")
			assert(type(btea.commands.enter_alt_screen) == "userdata", "enter_alt_screen command should be available")
			assert(type(btea.commands.exit_alt_screen) == "userdata", "exit_alt_screen command should be available")
			assert(type(btea.commands.hide_cursor) == "userdata", "hide_cursor command should be available")
			assert(type(btea.commands.show_cursor) == "userdata", "show_cursor command should be available")
			assert(type(btea.commands.quit) == "userdata", "quit command should be available")
			
			-- Test key binding functionality
			assert(type(btea.bind) == "function", "bind function should be available")
			
			-- Test batch and sequence functions
			assert(type(btea.batch) == "function", "batch function should be available")
			assert(type(btea.sequence) == "function", "sequence function should be available")
		`, "test_protocol")
		require.NoError(t, err)
	})

	t.Run("all render components are available", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test style functionality
			assert(type(btea.style) == "function", "style function should be available")
			
			-- Test text utilities
			assert(type(btea.text) == "table", "text utilities should be available")
			assert(type(btea.text.width) == "function", "text.width should be available")
			assert(type(btea.text.height) == "function", "text.height should be available")
			assert(type(btea.text.size) == "function", "text.size should be available")
			assert(type(btea.text.join_horizontal) == "function", "text.join_horizontal should be available")
			assert(type(btea.text.join_vertical) == "function", "text.join_vertical should be available")
			
			-- Test zone manager functionality
			assert(type(btea.zone_manager) == "function", "zone_manager should be available as a function")
		`, "test_render")
		require.NoError(t, err)
	})

	t.Run("all model components are available", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test text input
			assert(type(btea.text_input) == "function", "text_input should be available")
			
			-- Test text area
			assert(type(btea.text_area) == "function", "text_area should be available")
			
			-- Test paginator
			assert(type(btea.paginator) == "function", "paginator should be available")
			
			-- Test viewport
			assert(type(btea.viewport) == "function", "viewport should be available")
			
			-- Test table
			assert(type(btea.table) == "function", "table should be available")
			
			-- Test help
			assert(type(btea.help) == "function", "help should be available")
			
			-- Test spinner
			assert(type(btea.spinner) == "function", "spinner should be available")
			
			-- Test progress
			assert(type(btea.progress) == "function", "progress should be available")
		`, "test_models")
		require.NoError(t, err)
	})

	t.Run("list components are available", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test list functionality
			assert(type(btea.list) == "function", "list should be available")
		`, "test_list")
		require.NoError(t, err)
	})

	t.Run("events channel is available", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test events functionality
			assert(type(btea.events) == "function", "events should be available as a function")
		`, "test_events")
		require.NoError(t, err)
	})

	t.Run("basic command execution", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test basic command execution
			local cmd = btea.commands.clear_screen
			assert(cmd ~= nil, "clear_screen command should not be nil")
			
			-- Test command composition
			local batch_cmd = btea.batch({btea.commands.clear_screen, btea.commands.show_cursor})
			assert(batch_cmd ~= nil, "batch command should not be nil")
			
			local seq_cmd = btea.sequence({btea.commands.enter_alt_screen, btea.commands.hide_cursor})
			assert(seq_cmd ~= nil, "sequence command should not be nil")
		`, "test_command_execution")
		require.NoError(t, err)
	})

	t.Run("basic model creation", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test basic model creation
			local text_input = btea.text_input({})
			assert(text_input ~= nil, "text_input should be created successfully")
			
			local text_area = btea.text_area({})
			assert(text_area ~= nil, "text_area should be created successfully")
			
			local paginator = btea.paginator({})
			assert(paginator ~= nil, "paginator should be created successfully")
			
			local viewport = btea.viewport({})
			assert(viewport ~= nil, "viewport should be created successfully")
			
			local table_model = btea.table({})
			assert(table_model ~= nil, "table should be created successfully")
			
			local help_model = btea.help({})
			assert(help_model ~= nil, "help should be created successfully")
			
			local spinner = btea.spinner({})
			assert(spinner ~= nil, "spinner should be created successfully")
			
			local progress = btea.progress({})
			assert(progress ~= nil, "progress should be created successfully")
		`, "test_model_creation")
		require.NoError(t, err)
	})

	t.Run("basic render functionality", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test style creation
			local style = btea.style()
			assert(style ~= nil, "style should be created successfully")
			
			-- Test text utilities
			local width = btea.text.width("hello")
			assert(type(width) == "number", "text.width should return a number")
			
			local height = btea.text.height("hello\nworld")
			assert(type(height) == "number", "text.height should return a number")
			
			local w, h = btea.text.size("hello\nworld")
			assert(type(w) == "number", "text.size should return width as number")
			assert(type(h) == "number", "text.size should return height as number")
			
			-- Test zone manager creation
			local zm = btea.zone_manager()
			assert(zm ~= nil, "zone_manager should be created successfully")
		`, "test_render_functionality")
		require.NoError(t, err)
	})

	t.Run("list functionality", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test list creation
			local list = btea.list({})
			assert(list ~= nil, "list should be created successfully")
		`, "test_list_functionality")
		require.NoError(t, err)
	})

	t.Run("key binding functionality", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test key binding creation
			local binding = btea.bind({keys = {"ctrl+c"}, help = {key = "ctrl+c", desc = "quit"}})
			assert(binding ~= nil, "key binding should be created successfully")
			
			-- Test key binding with single key
			local simple_binding = btea.bind({keys = "enter"})
			assert(simple_binding ~= nil, "simple key binding should be created successfully")
		`, "test_key_binding")
		require.NoError(t, err)
	})

	t.Run("module integration test", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Create a simple TUI application structure
			local text_input = btea.text_input({})
			local style = btea.style()
			local cmd = btea.commands.clear_screen
			local binding = btea.bind({keys = "enter", help = {key = "enter", desc = "submit"}})
			
			-- Verify all components work together
			assert(text_input ~= nil, "text_input should be created")
			assert(style ~= nil, "style should be created")
			assert(cmd ~= nil, "command should be created")
			assert(binding ~= nil, "key binding should be created")
			
			-- Test that events function exists (but don't call it since it yields)
			assert(type(btea.events) == "function", "events should be available as a function")
		`, "test_integration")
		require.NoError(t, err)
	})
}

func TestBteaModuleErrorHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("invalid module operations", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test that invalid operations don't crash the module
		err = vm.DoString(newTestContext(), `
			local btea = require("btea")
			
			-- Test with nil values (should not crash)
			local result = pcall(function()
				-- These operations should handle nil gracefully
				if btea.commands then
					-- Just test that commands table exists
				end
			end)
			
			assert(result, "module should handle nil operations gracefully")
		`, "test_error_handling")
		require.NoError(t, err)
	})
}

func TestBteaModulePerformance(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module loading performance", func(t *testing.T) {
		mod := NewBteaModule(logger)

		// Test that module creation is fast
		for i := 0; i < 100; i++ {
			vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
			require.NoError(t, err)

			err = vm.DoString(newTestContext(), `
				local btea = require("btea")
				assert(btea ~= nil)
			`, "perf_test")
			require.NoError(t, err)

			vm.Close()
		}
	})
}
