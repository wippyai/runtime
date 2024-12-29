package pool

import (
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
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
	LoaderCalled bool
	name         string
}

func (m *mockModule) Loader(L *lua.LState) int {
	m.LoaderCalled = true
	return 0
}

func (m *mockModule) Name() string {
	return m.name
}

func TestVMConfigOptions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with module", func(t *testing.T) {
		mock := &mockModule{name: "test_module"}
		cfg := NewVMConfig(logger)
		opt := WithModule("test_module", mock)
		opt(cfg)

		mod, exists := cfg.Modules["test_module"]
		assert.True(t, exists)
		assert.Equal(t, mock, mod)
	})

	t.Run("with library", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		script := "return {test = function() return 'hello' end}"
		opt := WithLibrary("test_lib", script)
		opt(cfg)

		lib, exists := cfg.Libraries["test_lib"]
		assert.True(t, exists)
		assert.Equal(t, script, lib)
	})

	t.Run("with global value", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		value := lua.LString("test_value")
		opt := WithGlobalValue("test_global", value)
		opt(cfg)

		val, exists := cfg.Globals["test_global"]
		assert.True(t, exists)
		assert.Equal(t, value, val)
	})

	t.Run("with function", func(t *testing.T) {
		cfg := NewVMConfig(logger)
		script := "function test() return 'hello' end"
		opt := WithFunction("test_func", script)
		opt(cfg)

		fn, exists := cfg.Functions["test_func"]
		assert.True(t, exists)
		assert.Equal(t, script, fn)
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
		WithModule("test_module", mock)(cfg)

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
			function test(arg)
				return arg
			end
			return test
		`)(cfg)

		vm, err := CreateVM(cfg)
		require.NoError(t, err)
		defer vm.Close()

		// Test library loading
		err = vm.DoString(nil, `
			local lib = require("test_lib")
			assert(lib.test() == "hello")
		`, "test")
		assert.NoError(t, err)

		// Test global value
		err = vm.DoString(nil, `
			assert(test_global == "test")
		`, "test")
		assert.NoError(t, err)

		// Test function execution
		result, err := vm.Execute(nil, "test_func", lua.LString("hello"))
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello"), result)
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
		assert.Equal(t, lua.LString("value1"), cfg.Globals["key1"])
		assert.Equal(t, lua.LString("value2"), cfg.Globals["key2"])
		assert.Equal(t, "return {}", cfg.Libraries["lib1"])
	})
}
