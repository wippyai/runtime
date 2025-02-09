package manager

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewFunctions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates new instance", func(t *testing.T) {
		funcs := NewFunctions(logger)
		assert.NotNil(t, funcs)
		assert.NotNil(t, funcs.functions)
		assert.Empty(t, funcs.functions)
	})
}

func setupManagers(t *testing.T) (*Functions, *Modules, *Libraries) {
	logger := zap.NewNop()

	modules := NewModules(logger)
	libraries := NewLibraries(logger)
	functions := NewFunctions(logger)

	// Register test module
	module := &mockModule{name: "test_module"}
	err := modules.Register(module)
	require.NoError(t, err)

	// Add test library
	libCfg := &api.LibraryConfig{
		Source:  "return {test = function() return 'hello' end}",
		Meta:    registry.Metadata{"name": "test_lib"},
		Modules: []string{"test_module"},
	}
	err = libraries.Add("test_lib", libCfg)
	require.NoError(t, err)

	return functions, modules, libraries
}

func TestFunctions_Add(t *testing.T) {
	functions, modules, libraries := setupManagers(t)

	t.Run("adds new function successfully", func(t *testing.T) {
		cfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}
		err := function.Add("test_func", cfg, modules, libraries)
		require.NoError(t, err)

		// Verify function was stored
		stored, exists := function.Get("test_func")
		assert.True(t, exists)
		assert.Equal(t, cfg, stored)
	})

	t.Run("fails with missing module dependency", func(t *testing.T) {
		cfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"test_lib"},
			Modules:   []string{"non_existent_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}
		err := function.Add("new_func", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module non_existent_module not found")
	})

	t.Run("fails with missing library dependency", func(t *testing.T) {
		cfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"non_existent_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}
		err := function.Add("new_func", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "library non_existent_lib not found")
	})
}

func TestFunctions_Update(t *testing.T) {
	functions, modules, libraries := setupManagers(t)

	// First add a function
	initialCfg := &api.FunctionConfig{
		Source:    "function test() return 'hello' end",
		Method:    "test",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}
	err := function.Add("test_func", initialCfg, modules, libraries)
	require.NoError(t, err)

	t.Run("updates existing function", func(t *testing.T) {
		updatedCfg := &api.FunctionConfig{
			Source:    "function test() return 'updated' end",
			Method:    "test",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 2, Workers: 2},
			Meta:      registry.Metadata{},
		}
		err := function.Update("test_func", updatedCfg, modules, libraries)
		require.NoError(t, err)

		// Verify function was updated
		stored, exists := function.Get("test_func")
		assert.True(t, exists)
		assert.Equal(t, updatedCfg, stored)
	})

	t.Run("fails updating non-existent function", func(t *testing.T) {
		cfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}
		err := function.Update("non_existent", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestFunctions_Delete(t *testing.T) {
	functions, modules, libraries := setupManagers(t)

	// First add a function
	cfg := &api.FunctionConfig{
		Source:    "function test() return 'hello' end",
		Method:    "test",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}
	err := function.Add("test_func", cfg, modules, libraries)
	require.NoError(t, err)

	t.Run("deletes existing function", func(t *testing.T) {
		err := function.Delete("test_func")
		require.NoError(t, err)

		// Verify function was deleted
		_, exists := function.Get("test_func")
		assert.False(t, exists)
	})

	t.Run("fails deleting non-existent function", func(t *testing.T) {
		err := function.Delete("non_existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestFunctions_Clone(t *testing.T) {
	functions, modules, libraries := setupManagers(t)

	// Add a test function
	cfg := &api.FunctionConfig{
		Source:    "function test() return 'hello' end",
		Method:    "test",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}
	err := function.Add("test_func", cfg, modules, libraries)
	require.NoError(t, err)

	t.Run("creates exact copy of functions", func(t *testing.T) {
		cloned := function.Clone()

		// Verify cloned instance has same functions
		assert.Equal(t, len(function.functions), len(cloned.functions))

		original, exists := function.Get("test_func")
		assert.True(t, exists)

		clonedCfg, exists := cloned.Get("test_func")
		assert.True(t, exists)
		assert.Equal(t, original, clonedCfg)
	})

	t.Run("modifications don't affect original", func(t *testing.T) {
		cloned := function.Clone()

		// Modify cloned instance
		err := cloned.Delete("test_func")
		require.NoError(t, err)

		// Verify original is unchanged
		_, exists := function.Get("test_func")
		assert.True(t, exists)

		// Verify cloned instance was modified
		_, exists = cloned.Get("test_func")
		assert.False(t, exists)
	})
}

func TestFunctions_FindDependentOnLibrary(t *testing.T) {
	functions, modules, libraries := setupManagers(t)

	// Add two functions with different dependencies
	cfg1 := &api.FunctionConfig{
		Source:    "function test1() return 'hello' end",
		Method:    "test1",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}
	cfg2 := &api.FunctionConfig{
		Source:    "function test2() return 'world' end",
		Method:    "test2",
		Libraries: []string{},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}

	err := function.Add("func1", cfg1, modules, libraries)
	require.NoError(t, err)
	err = function.Add("func2", cfg2, modules, libraries)
	require.NoError(t, err)

	t.Run("finds dependent functions", func(t *testing.T) {
		dependent := function.FindDependentOnLibrary("test_lib")
		assert.Len(t, dependent, 1)
		fn, exists := dependent["func1"]
		assert.True(t, exists)
		assert.Equal(t, cfg1, fn)
	})

	t.Run("returns empty map for no dependents", func(t *testing.T) {
		dependent := function.FindDependentOnLibrary("non_existent_lib")
		assert.Empty(t, dependent)
	})
}

func TestFunctions_MakeFactory(t *testing.T) {
	functions, modules, libraries := setupManagers(t)
	logger := zap.NewNop()

	// Add a function
	cfg := &api.FunctionConfig{
		Source:    "function test() return 'hello' end",
		Method:    "test",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Pool:      api.PoolConfig{Size: 1, Workers: 1},
		Meta:      registry.Metadata{},
	}

	t.Run("creates factory successfully", func(t *testing.T) {
		factory, err := function.MakeFactory("test_func", cfg, logger, modules, libraries)
		require.NoError(t, err)
		assert.NotNil(t, factory)
	})

	t.Run("fails with invalid dependencies", func(t *testing.T) {
		invalidCfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"non_existent_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}

		factory, err := function.MakeFactory("test_func", invalidCfg, logger, modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "library non_existent_lib not found")
	})
}
