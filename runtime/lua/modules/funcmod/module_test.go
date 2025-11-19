package funcmod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestFuncmodModule(t *testing.T) {
	t.Run("module provides complete process API", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register only the function_api module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "function_api", module.Name())

		// Load function_api as complete process API
		err = vm.State().DoString(`
			local process = require("function_api")
			
			-- Check that all core process functions exist
			assert(process ~= nil, "process table should exist")
			assert(type(process.id) == "function", "process.id should be a function")
			assert(type(process.pid) == "function", "process.pid should be a function")
			assert(type(process.send) == "function", "process.send should be a function")
			assert(type(process.spawn) == "function", "process.spawn should be a function")
			assert(type(process.terminate) == "function", "process.terminate should be a function")
			
			-- Check that function-specific methods exist
			assert(type(process.inbox) == "function", "process.inbox should be a function")
			assert(type(process.events) == "function", "process.events should be a function")
			assert(type(process.listen) == "function", "process.listen should be a function")
			assert(type(process.unlisten) == "function", "process.unlisten should be a function")
			
			-- Check that registry and events exist
			assert(process.registry ~= nil, "process.registry should exist")
			assert(process.event ~= nil, "process.event should exist")
			assert(type(process.registry.register) == "function", "process.registry.register should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("table is immutable", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that the table is immutable
		err = vm.State().DoString(`
			local process = require("function_api")
			
			-- This should fail because table is immutable
			process.test_field = "should fail"
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "attempt to modify Immutable table")
	})

	t.Run("same table instance across requires", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that multiple requires return the same table
		err = vm.State().DoString(`
			local process1 = require("function_api")
			local process2 = require("function_api")
			
			-- Should be the same table reference
			assert(process1 == process2, "should return same table instance")
		`)
		require.NoError(t, err)
	})

	t.Run("listen rejects empty topic", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with empty topic
		err = vm.State().DoString(`
			local process = require("function_api")
			
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
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with @ topic
		err = vm.State().DoString(`
			local process = require("function_api")

			local ch, err = process.listen("@test")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use @ topics")
	})

	t.Run("unlisten requires channel parameter", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that unlisten fails without channel
		err = vm.State().DoString(`
			local process = require("function_api")

			local result = process.unlisten()
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad argument")
	})

	t.Run("unlisten rejects invalid channel", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that unlisten fails with non-channel argument
		err = vm.State().DoString(`
			local process = require("function_api")

			local result = process.unlisten("not a channel")
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "userdata expected")
	})
}

func TestFuncmodModuleErrorHandling(t *testing.T) {
	t.Run("functions fail without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that functions fail without proper context setup
		err = vm.State().DoString(`
			local process = require("function_api")
			
			local ch, err = process.listen("test_topic")
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set up message handling")
	})

	t.Run("inbox fails without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewFunctionAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		err = vm.State().DoString(`
			local process = require("function_api")
			
			local inbox, err = process.inbox()
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set up message handling")
	})
}
