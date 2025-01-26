package manager

import (
	"github.com/ponyruntime/pony/api/payload"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func makeTestFunctionEntry(id string, cfg *api.FunctionConfig) registry.Entry {
	return registry.Entry{
		ID:   registry.ID(id),
		Kind: api.KindFunction,
		Meta: registry.Metadata{},
		Data: payload.NewPayload(cfg, payload.Golang),
	}
}

func TestNewFunctions(t *testing.T) {
	logger := zap.NewNop()
	dtt := makeTestTranscoder()

	t.Run("creates new instance", func(t *testing.T) {
		funcs := NewFunctions(dtt, logger)
		assert.NotNil(t, funcs)
		assert.NotNil(t, funcs.functions)
		assert.Empty(t, funcs.functions)
	})
}

func setupManagers(t *testing.T) (*Functions, *Modules, *Libraries) {
	logger := zap.NewNop()
	dtt := makeTestTranscoder()

	modules := NewModules(logger)
	libraries := NewLibraries(dtt, logger)
	functions := NewFunctions(dtt, logger)

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
	err = libraries.Add(nil, makeTestEntry("test_lib", libCfg))
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
		err := functions.Add(makeTestFunctionEntry("test_func", cfg), modules, libraries)
		require.NoError(t, err)

		// Verify function was stored
		stored, exists := functions.Get("test_func")
		assert.True(t, exists)
		assert.Equal(t, cfg, stored)
	})

	t.Run("fails adding duplicate function", func(t *testing.T) {
		cfg := &api.FunctionConfig{
			Source:    "function test() return 'hello' end",
			Method:    "test",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Pool:      api.PoolConfig{Size: 1, Workers: 1},
			Meta:      registry.Metadata{},
		}
		err := functions.Add(makeTestFunctionEntry("test_func", cfg), modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
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
		err := functions.Add(makeTestFunctionEntry("new_func", cfg), modules, libraries)
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
		err := functions.Add(makeTestFunctionEntry("new_func", cfg), modules, libraries)
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
	err := functions.Add(makeTestFunctionEntry("test_func", initialCfg), modules, libraries)
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
		err := functions.Update(makeTestFunctionEntry("test_func", updatedCfg), modules, libraries)
		require.NoError(t, err)

		// Verify function was updated
		stored, exists := functions.Get("test_func")
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
		err := functions.Update(makeTestFunctionEntry("non_existent", cfg), modules, libraries)
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
	err := functions.Add(makeTestFunctionEntry("test_func", cfg), modules, libraries)
	require.NoError(t, err)

	t.Run("deletes existing function", func(t *testing.T) {
		err := functions.Delete(makeTestFunctionEntry("test_func", nil))
		require.NoError(t, err)

		// Verify function was deleted
		_, exists := functions.Get("test_func")
		assert.False(t, exists)
	})

	t.Run("fails deleting non-existent function", func(t *testing.T) {
		err := functions.Delete(makeTestFunctionEntry("non_existent", nil))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
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

	err := functions.Add(makeTestFunctionEntry("func1", cfg1), modules, libraries)
	require.NoError(t, err)
	err = functions.Add(makeTestFunctionEntry("func2", cfg2), modules, libraries)
	require.NoError(t, err)

	t.Run("finds dependent functions", func(t *testing.T) {
		dependent := functions.FindDependentOnLibrary("test_lib")
		assert.Len(t, dependent, 1)
		assert.Contains(t, dependent, registry.ID("func1"))
	})

	t.Run("returns empty slice for no dependents", func(t *testing.T) {
		dependent := functions.FindDependentOnLibrary("non_existent_lib")
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
	err := functions.Add(makeTestFunctionEntry("test_func", cfg), modules, libraries)
	require.NoError(t, err)

	t.Run("creates factory successfully", func(t *testing.T) {
		factory, err := functions.MakeFactory("test_func", logger, modules, libraries)
		require.NoError(t, err)
		assert.NotNil(t, factory)
	})

	t.Run("fails with non-existent function", func(t *testing.T) {
		factory, err := functions.MakeFactory("non_existent", logger, modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "not found")
	})
}
