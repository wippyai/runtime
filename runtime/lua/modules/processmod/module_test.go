package processmod

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessmodModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)
		processModule := processmod.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the process module first
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)

		// Register the processmod module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "process_api", module.Name())

		// Load the module and check that process functions are registered
		err = vm.State().DoString(`
			-- Load the process module first to create the process table
			local process = require("process")
			
			-- Set it as a global variable so processmod can find it
			_G.process = process
			
			-- Load the processmod module (this should register functions with process table)
			require("process_api")
			
			-- Check that process table exists and has our functions
			assert(process ~= nil, "process table should exist")
			assert(type(process.inbox) == "function", "process.inbox should be a function")
			assert(type(process.events) == "function", "process.events should be a function")
			assert(type(process.listen) == "function", "process.listen should be a function")
			assert(type(process.get_options) == "function", "process.get_options should be a function")
			assert(type(process.set_options) == "function", "process.set_options should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("listen rejects empty topic", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)
		processModule := processmod.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with empty topic
		err = vm.State().DoString(`
			local process = require("process")
			_G.process = process
			require("process_api")
			
			-- This should fail because topic is empty
			local ch, err = process.listen("")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "topic cannot be empty")
	})

	t.Run("listen rejects @ topics", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)
		processModule := processmod.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with @ topic
		err = vm.State().DoString(`
			local process = require("process")
			_G.process = process
			require("process_api")
			
			-- This should fail because topic starts with @
			local ch, err = process.listen("@test")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use @ topics")
	})

	t.Run("functions fail without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)
		processModule := processmod.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that functions fail without proper context setup
		err = vm.State().DoString(`
			local process = require("process")
			_G.process = process
			require("process_api")
			
			-- This should fail because there's no unit of work
			local inbox, err = process.inbox()
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no unit of work found")
	})
}
