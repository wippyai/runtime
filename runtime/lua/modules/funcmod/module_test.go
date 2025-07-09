package funcmod

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFuncmodModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)
		processModule := process.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "function_api", module.Name())

		// Load the module and check that process functions are registered
		err = vm.State().DoString(`
			-- Load the process module first to create the process table
			process = require("process")
			
			-- Load the module (this should register functions with process table)
			require("function_api")
			
			-- Check that process table exists and has our functions
			assert(process ~= nil, "process table should exist")
			assert(type(process.inbox) == "function", "process.inbox should be a function")
			assert(type(process.events) == "function", "process.events should be a function")
			assert(type(process.listen) == "function", "process.listen should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("lazyListen rejects empty topic", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)
		processModule := process.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails without proper context setup
		err = vm.State().DoString(`
			process = require("process")
			require("function_api")
			
			local ch, err = process.listen("")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set up message handling")
	})

	t.Run("lazyListen rejects @ topics", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)
		processModule := process.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails without proper context setup
		err = vm.State().DoString(`
			process = require("process")
			require("function_api")
			
			local ch, err = process.listen("@test")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set up message handling")
	})
}

func TestFuncmodModuleErrorHandling(t *testing.T) {
	t.Run("lazyListen with no unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)
		processModule := process.NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register modules
		vm.State().PreloadModule(processModule.Name(), processModule.Loader)
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails without proper context setup
		err = vm.State().DoString(`
			process = require("process")
			require("function_api")
			
			local ch, err = process.listen("test_topic")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set up message handling")
	})
}
