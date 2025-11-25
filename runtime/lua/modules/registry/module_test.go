package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestRegistryModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Info().Name, module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "registry", module.Info().Name)

		// Load the module and check that functions are registered
		err = vm.State().DoString(`
			local registry = require("registry")
			
			-- Check that core functions exist
			assert(type(registry.snapshot) == "function", "registry.snapshot should be a function")
			assert(type(registry.snapshot_at) == "function", "registry.snapshot_at should be a function")
			assert(type(registry.current_version) == "function", "registry.current_version should be a function")
			assert(type(registry.versions) == "function", "registry.versions should be a function")
			assert(type(registry.apply_version) == "function", "registry.apply_version should be a function")
			assert(type(registry.parse_id) == "function", "registry.parse_id should be a function")
			assert(type(registry.history) == "function", "registry.history should be a function")
			assert(type(registry.find) == "function", "registry.find should be a function")
			assert(type(registry.get) == "function", "registry.get should be a function")
			assert(type(registry.build_delta) == "function", "registry.build_delta should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("parse_id creates ID from string", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Info().Name, module.Loader)

		// Test parse_id function
		err = vm.State().DoString(`
			local registry = require("registry")
			local id = registry.parse_id("test:example")
			
			-- Check that we got a table
			assert(type(id) == "table", "id should be a table")
			assert(id.ns == "test", "ns should be 'test'")
			assert(id.name == "example", "name should be 'example'")
		`)
		require.NoError(t, err)
	})

	t.Run("parse_id handles invalid format", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Info().Name, module.Loader)

		// Test parse_id function with invalid format
		err = vm.State().DoString(`
			local registry = require("registry")
			local id = registry.parse_id("invalid_format")
			
			-- Should still return a table with empty values
			assert(type(id) == "table", "id should be a table")
			assert(id.ns == "", "ns should be empty for invalid format")
			assert(id.name == "invalid_format", "name should be the full string for invalid format")
		`)
		require.NoError(t, err)
	})
}
