package processmod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestProcessmodModule(t *testing.T) {
	t.Run("module provides complete process API", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register only the process_api module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "process_api", module.Name())

		// Load process_api as complete process API
		err = vm.State().DoString(`
			local process = require("process_api")
			
			-- Check that all core process functions exist
			assert(process ~= nil, "process table should exist")
			assert(type(process.id) == "function", "process.id should be a function")
			assert(type(process.pid) == "function", "process.pid should be a function")
			assert(type(process.send) == "function", "process.send should be a function")
			assert(type(process.spawn) == "function", "process.spawn should be a function")
			assert(type(process.terminate) == "function", "process.terminate should be a function")
			
			-- Check that process-specific methods exist
			assert(type(process.inbox) == "function", "process.inbox should be a function")
			assert(type(process.events) == "function", "process.events should be a function")
			assert(type(process.listen) == "function", "process.listen should be a function")
			assert(type(process.unlisten) == "function", "process.unlisten should be a function")
			assert(type(process.get_options) == "function", "process.get_options should be a function")
			assert(type(process.set_options) == "function", "process.set_options should be a function")
			
			-- Check that registry and events exist
			assert(process.registry ~= nil, "process.registry should exist")
			assert(process.event ~= nil, "process.event should exist")
			assert(type(process.registry.register) == "function", "process.registry.register should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("table is immutable", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that the table is immutable
		err = vm.State().DoString(`
			local process = require("process_api")
			
			-- This should fail because table is immutable
			process.test_field = "should fail"
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "attempt to modify Immutable table")
	})

	t.Run("same table instance across requires", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that multiple requires return the same table
		err = vm.State().DoString(`
			local process1 = require("process_api")
			local process2 = require("process_api")
			
			-- Should be the same table reference
			assert(process1 == process2, "should return same table instance")
		`)
		require.NoError(t, err)
	})

	t.Run("listen rejects empty topic", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with empty topic
		err = vm.State().DoString(`
			local process = require("process_api")
			
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

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that listen fails with @ topic
		err = vm.State().DoString(`
			local process = require("process_api")

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
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that unlisten fails without channel
		err = vm.State().DoString(`
			local process = require("process_api")

			local result = process.unlisten()
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad argument")
	})

	t.Run("unlisten rejects invalid channel", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that unlisten fails with non-channel argument
		err = vm.State().DoString(`
			local process = require("process_api")

			local result = process.unlisten("not a channel")
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "userdata expected")
	})
}

func TestProcessmodModuleErrorHandling(t *testing.T) {
	t.Run("functions fail without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test that functions fail without proper context setup
		err = vm.State().DoString(`
			local process = require("process_api")
			
			local inbox, err = process.inbox()
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no unit of work found")
	})

	t.Run("get_options fails without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		err = vm.State().DoString(`
			local process = require("process_api")
			
			local options, err = process.get_options()
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no unit of work found")
	})

	t.Run("set_options fails without unit of work", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		vm.State().PreloadModule(module.Name(), module.Loader)

		err = vm.State().DoString(`
			local process = require("process_api")
			
			local success, err = process.set_options({trap_links = true})
			if err then
				error(err)
			end
		`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no unit of work found")
	})
}
