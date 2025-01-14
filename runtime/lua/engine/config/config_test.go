package config

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestNewVMConfig(t *testing.T) {
	logger := zap.NewNop()

	t.Run("default configuration", func(t *testing.T) {
		cfg := NewVMConfig(logger)

		assert.NotNil(t, cfg)
		assert.NotNil(t, cfg.Modules)
		assert.NotNil(t, cfg.Libraries)
		assert.NotNil(t, cfg.Globals)
		assert.NotNil(t, cfg.Functions)
		assert.NotNil(t, cfg.EngineOpts)
		assert.Equal(t, logger, cfg.Logger)

		// Check maps are empty but initialized
		assert.Empty(t, cfg.Modules)
		assert.Empty(t, cfg.Libraries)
		assert.Empty(t, cfg.Globals)
		assert.Empty(t, cfg.Functions)
		assert.Empty(t, cfg.EngineOpts)
	})
}

type mockModule struct {
	name string
}

func (m *mockModule) Name() string {
	return m.name
}

func (m *mockModule) Loader(L *lua.LState) int {
	return 0
}

func TestVMConfigOptions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with module", func(t *testing.T) {
		mock := &mockModule{name: "test_module"}
		cfg := NewVMConfig(logger)
		opt := WithModule(mock)
		opt(cfg)

		assert.Len(t, cfg.Modules, 1)
		assert.Equal(t, mock, cfg.Modules[0])
	})

	t.Run("with library", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		script := "return {test = function() return 'hello' end}"
		opt := WithLibrary("test_lib", script)
		opt(cfg)

		assert.Len(t, cfg.Libraries, 1)
		assert.Equal(t, "test_lib", cfg.Libraries[0].Name)
		assert.Equal(t, script, cfg.Libraries[0].Script)
	})

	t.Run("with global value", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		value := lua.LString("test_value")
		opt := WithGlobalValue("test_global", value)
		opt(cfg)

		assert.Len(t, cfg.Globals, 1)
		assert.Equal(t, "test_global", cfg.Globals[0].Name)
		assert.Equal(t, value, cfg.Globals[0].Value)
	})

	t.Run("with function", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		script := "function test() return 'hello' end"
		opt := WithFunction("test_func", script)
		opt(cfg)

		assert.Len(t, cfg.Functions, 1)
		assert.Equal(t, "test_func", cfg.Functions[0].Name)
		assert.Equal(t, script, cfg.Functions[0].Script)
	})

	t.Run("with engine options", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		opt1 := func(*engine.VM) {}
		opt2 := func(*engine.VM) {}

		engOpt := WithEngineOptions(opt1, opt2)
		engOpt(cfg)

		assert.Len(t, cfg.EngineOpts, 2)
	})
}

func TestCreateVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create empty VM", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		vm, err := CreateVM(cfg)

		require.NoError(t, err)
		require.NotNil(t, vm)
		defer vm.Close()
	})

	t.Run("create VM with all options", func(t *testing.T) {
		cfg := NewVMConfig(logger)

		// Add a module
		mock := &mockModule{name: "test_module"}
		WithModule(mock)(cfg)

		// Add a library
		WithLibrary("test_lib", `
			local lib = {}
			function lib.test() return "hello" end
			return lib
		`)(cfg)

		// Add a global
		WithGlobalValue("test_global", lua.LString("test"))(cfg)

		// Add a function
		WithFunction("test_func", `
			function test_func(arg)
				return arg
			end
		`)(cfg)

		vm, err := CreateVM(cfg)
		require.NoError(t, err)
		defer vm.Close()

		// Test library loading
		err = vm.DoString(nil, "", `
			local lib = require("test_lib")
			assert(lib.test() == "hello")
		`)
		assert.NoError(t, err)

		// Test global value
		err = vm.DoString(nil, "", `
			assert(test_global == "test")
		`)
		assert.NoError(t, err)

		// Test function execution
		err = vm.DoString(nil, "", `
			assert(test("hello") == "hello")
		`)
		assert.NoError(t, err)

	})

	t.Run("error on invalid function", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		WithFunction("invalid", "this is not valid lua")(cfg)

		vm, err := CreateVM(cfg)
		assert.Error(t, err)
		assert.Nil(t, vm)
	})
}

func TestVMConfigChaining(t *testing.T) {
	logger := zap.NewNop()

	t.Run("multiple options", func(t *testing.T) {
		cfg := NewVMConfig(logger)

		// Apply multiple options
		opt1 := WithGlobalValue("key1", lua.LString("value1"))
		opt2 := WithGlobalValue("key2", lua.LString("value2"))
		opt3 := WithLibrary("lib1", "return {}")

		opt1(cfg)
		opt2(cfg)
		opt3(cfg)

		assert.Len(t, cfg.Globals, 2)
		assert.Len(t, cfg.Libraries, 1)

		// Verify values
		assert.Equal(t, "key1", cfg.Globals[0].Name)
		assert.Equal(t, lua.LString("value1"), cfg.Globals[0].Value)
		assert.Equal(t, "key2", cfg.Globals[1].Name)
		assert.Equal(t, lua.LString("value2"), cfg.Globals[1].Value)
		assert.Equal(t, "lib1", cfg.Libraries[0].Name)
		assert.Equal(t, "return {}", cfg.Libraries[0].Script)
	})
}
