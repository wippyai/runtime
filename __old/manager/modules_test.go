package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockModule implements api.Module for testing
type mockModule struct {
	name string
}

func (m *mockModule) Name() string {
	return m.name
}

func (m *mockModule) Loader(*lua.LState) int {
	return 0
}

func TestNewModules(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates new instance", func(t *testing.T) {
		modules := NewModules(logger)
		assert.NotNil(t, modules)
		assert.NotNil(t, modules.modules)
		assert.Empty(t, modules.modules)
	})
}

func TestModules_Register(t *testing.T) {
	logger := zap.NewNop()
	modules := NewModules(logger)

	t.Run("registers new module successfully", func(t *testing.T) {
		module := &mockModule{name: "test1"}
		err := modules.Register(module)
		require.NoError(t, err)

		// Verify module was stored
		stored, err := modules.Get("test1")
		assert.NoError(t, err)
		assert.Equal(t, module, stored)
	})

	t.Run("fails registering duplicate module", func(t *testing.T) {
		module := &mockModule{name: "test1"}
		err := modules.Register(module)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("registers multiple modules", func(t *testing.T) {
		module2 := &mockModule{name: "test2"}
		module3 := &mockModule{name: "test3"}

		err := modules.Register(module2)
		require.NoError(t, err)
		err = modules.Register(module3)
		require.NoError(t, err)

		// Verify all modules exist
		names := modules.List()
		assert.Len(t, names, 3)
		assert.Contains(t, names, "test1")
		assert.Contains(t, names, "test2")
		assert.Contains(t, names, "test3")
	})
}

func TestModules_Unregister(t *testing.T) {
	logger := zap.NewNop()
	modules := NewModules(logger)
	module := &mockModule{name: "test"}

	t.Run("unregisters existing module", func(t *testing.T) {
		// First register a module
		err := modules.Register(module)
		require.NoError(t, err)

		// Then unregister it
		err = modules.Unregister("test")
		require.NoError(t, err)

		// Verify it's gone
		_, err = modules.Get("test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("fails unregistering non-existent module", func(t *testing.T) {
		err := modules.Unregister("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestModules_Get(t *testing.T) {
	logger := zap.NewNop()
	modules := NewModules(logger)
	module := &mockModule{name: "test"}

	t.Run("gets existing module", func(t *testing.T) {
		err := modules.Register(module)
		require.NoError(t, err)

		stored, err := modules.Get("test")
		assert.NoError(t, err)
		assert.Equal(t, module, stored)
	})

	t.Run("returns error for non-existent module", func(t *testing.T) {
		stored, err := modules.Get("non-existent")
		assert.Error(t, err)
		assert.Nil(t, stored)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestModules_Has(t *testing.T) {
	logger := zap.NewNop()
	modules := NewModules(logger)
	module := &mockModule{name: "test"}

	t.Run("returns true for existing module", func(t *testing.T) {
		err := modules.Register(module)
		require.NoError(t, err)

		exists := modules.Has("test")
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent module", func(t *testing.T) {
		exists := modules.Has("non-existent")
		assert.False(t, exists)
	})
}

func TestModules_List(t *testing.T) {
	logger := zap.NewNop()
	modules := NewModules(logger)

	t.Run("returns empty list when no modules", func(t *testing.T) {
		names := modules.List()
		assert.Empty(t, names)
	})

	t.Run("returns all module names", func(t *testing.T) {
		// Register some modules
		module1 := &mockModule{name: "test1"}
		module2 := &mockModule{name: "test2"}

		err := modules.Register(module1)
		require.NoError(t, err)
		err = modules.Register(module2)
		require.NoError(t, err)

		// Create the list
		names := modules.List()
		assert.Len(t, names, 2)
		assert.Contains(t, names, "test1")
		assert.Contains(t, names, "test2")
	})
}
