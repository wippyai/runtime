package engine

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create new VM", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		assert.NotNil(t, vm)
		defer vm.Close()
	})

	t.Run("compile and execute simple function", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
		function test(arg)
			return arg
		end
		return test
		`

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		arg := lua.LString("hello world")
		result, err := vm.Execute(context.Background(), "test", arg)
		assert.NoError(t, err)
		assert.Equal(t, arg, result)
	})

	t.Run("compile and execute simple function by name", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
		function test(arg)
			return arg
		end
		`

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		arg := lua.LString("hello world")
		result, err := vm.Execute(context.Background(), "test", arg)
		assert.NoError(t, err)
		assert.Equal(t, arg, result)
	})

	t.Run("compile no func", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := ``

		err = vm.CompileFunction("test", script)
		assert.Error(t, err)
	})

	// Add these to TestVM_Basic after the existing test cases

	t.Run("global function without matching name", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    function different_name()
        return "hello world"
    end
    `

		err = vm.CompileFunction("test", script)
		assert.Error(t, err)
	})

	t.Run("local function with return", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    local function test()
        return "hello world"
    end
    return test
    `

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello world"), result)
	})

	t.Run("module style declaration", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    local M = {}
    function M.test()
        return "hello world"
    end
    return M
    `

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello world"), result)
	})

	t.Run("anonymous function assignment", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    test = function()
        return "hello world"
    end
    `

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello world"), result)
	})

	t.Run("local with module and direct return", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    local M = {}
    function M.other()
        return "other"
    end
    function test()
        return "hello world"
    end
    return test
    `

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello world"), result)
	})

	t.Run("mixed global and local declarations", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
    local function helper()
        return "helper"
    end
    
    function test()
        return helper()
    end
    `

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("helper"), result)
	})

	t.Run("execute function with table argument", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
		function test(args)
			return args.message
		end
		return test
		`

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		tbl := lua.LTable{}
		tbl.RawSetString("message", lua.LString("hello"))

		result, err := vm.Execute(context.Background(), "test", &tbl)
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("hello"), result)
	})
}

func TestVM_Options(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with global function", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalFunction("test", func(L *lua.LState) int {
			L.Push(lua.LString("global"))
			return 1
		}))
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `assert(test() == "global")`, "test")
		assert.NoError(t, err)
	})

	t.Run("with global value", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalValue("TEST_VALUE", lua.LString("test")))
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `assert(TEST_VALUE == "test")`, "test")
		assert.NoError(t, err)
	})

	t.Run("with library", func(t *testing.T) {
		libSource := `
		local lib = {}
		function lib.test()
			return "library"
		end
		return lib
		`

		vm, err := NewVM(logger, WithLibrary("testlib", libSource))
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
		local lib = require("testlib")
		assert(lib.test() == "library")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestVM_Errors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("execute non-existent function", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		_, err = vm.Execute(context.Background(), "nonexistent", lua.LNil)
		assert.Error(t, err)
	})

	t.Run("compile invalid script", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.CompileFunction("invalid", "this is not valid lua")
		assert.Error(t, err)
	})

	t.Run("execute script with runtime error", func(t *testing.T) {
		vm, err := NewVM(logger)
		assert.NoError(t, err)
		defer vm.Close()

		script := `
		function test()
			error("runtime error")
		end
		return test
		`

		err = vm.CompileFunction("test", script)
		assert.NoError(t, err)

		_, err = vm.Execute(context.Background(), "test", lua.LNil)
		assert.Error(t, err)
	})
}

func TestVM_GlobalState(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Set up initial global state
	err = vm.DoString(nil, `
        state = {count = 0}
        function increment()
            state.count = state.count + 1
            return state.count
        end
        function getCount()
            return state.count
        end
    `, "setup")
	require.NoError(t, err)

	// First increment should return 1
	err = vm.DoString(nil, `assert(increment() == 1)`, "test1")
	require.NoError(t, err)

	// Second increment should return 2
	err = vm.DoString(nil, `assert(increment() == 2)`, "test2")
	require.NoError(t, err)

	// Get count should return the current value (2)
	err = vm.DoString(nil, `assert(getCount() == 2)`, "test3")
	require.NoError(t, err)

	// Verify state persists even with new chunk execution
	err = vm.DoString(nil, `assert(state.count == 2)`, "test4")
	require.NoError(t, err)
}

func TestVM_CompiledGlobalState(t *testing.T) {
	logger := zap.NewNop()

	// Create initial state table
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	// Create VM with global state
	vm, err := NewVM(logger, WithGlobalValue("state", stateTable))
	require.NoError(t, err)
	defer vm.Close()

	// Compile increment function
	err = vm.CompileFunction("increment", `
        function increment()
            state.count = state.count + 1
            return state.count
        end
        return increment
    `)
	require.NoError(t, err)

	// Compile getCount function
	err = vm.CompileFunction("getCount", `
        function getCount()
            return state.count
        end
        return getCount
    `)
	require.NoError(t, err)

	// First increment should return 1
	result, err := vm.Execute(context.Background(), "increment", lua.LNil)
	require.NoError(t, err)
	assert.Equal(t, lua.LNumber(1), result)

	// Second increment should return 2
	result, err = vm.Execute(context.Background(), "increment", lua.LNil)
	require.NoError(t, err)
	assert.Equal(t, lua.LNumber(2), result)

	// GetCount should return current value (2)
	result, err = vm.Execute(context.Background(), "getCount", lua.LNil)
	require.NoError(t, err)
	assert.Equal(t, lua.LNumber(2), result)

	// Verify final state directly through Lua state
	globalState := vm.State().GetGlobal("state").(*lua.LTable)
	count := globalState.RawGetString("count")
	assert.Equal(t, lua.LNumber(2), count)
}

func TestVM_ErrorTraceback(t *testing.T) {
	logger := zap.NewNop()

	libSource := `
        local lib = {}
        function lib.divide(a, b)
            if b == 0 then
                error("division by zero in library function")
            end
            return a / b
        end
        return lib
    `

	vm, err := NewVM(logger, WithLibrary("mathlib", libSource))
	require.NoError(t, err)
	defer vm.Close()

	t.Run("error in DoString with traceback", func(t *testing.T) {
		err := vm.DoString(nil, `
            local function deep()
                error("deep error")
            end
            local function middle()
                deep()
            end
            local function top()
                middle()
            end
            top()
        `, "error_test")

		require.Error(t, err)
		errStr := err.Error()
		// Check error content
		assert.Contains(t, errStr, "deep error")
		// Check function names in trace
		assert.Contains(t, errStr, "in function 'deep'")
		assert.Contains(t, errStr, "in function 'middle'")
		assert.Contains(t, errStr, "in function 'top'")
		// Check script name and line numbers
		assert.Contains(t, errStr, "<error_test>:3:")
		assert.Contains(t, errStr, "<error_test>:6:")
		assert.Contains(t, errStr, "<error_test>:9:")
	})

	t.Run("error in compiled function", func(t *testing.T) {
		err := vm.CompileFunction("bad_function", `
            function test()
                local x = nil
                return x.nonexistent
            end
            return test
        `)
		require.NoError(t, err)

		_, err = vm.Execute(context.Background(), "bad_function", lua.LNil)
		require.Error(t, err)
		errStr := err.Error()
		// Check exact error message
		assert.Contains(t, errStr, "attempt to index a non-table object(nil)")
		assert.Contains(t, errStr, "bad_function:4:")
	})

	t.Run("syntax error in compile", func(t *testing.T) {
		err := vm.CompileFunction("syntax_error", `
            function test()
                if true then
                    print("missing end")
            return test
        `)
		require.Error(t, err)
		errStr := err.Error()
		// Check for generic syntax error
		assert.Contains(t, errStr, "syntax error")
		assert.Contains(t, errStr, "syntax_error")
	})
}

// BenchmarkNewVM benchmarks the creation of a new VM instance.
func BenchmarkNewVM(b *testing.B) {
	logger := zap.NewNop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		vm, err := NewVM(logger)
		if err != nil {
			b.Fatal(err)
		}
		vm.Close()
	}
}

// BenchmarkCompileFunction benchmarks the compilation of a Lua function.
func BenchmarkCompileFunction(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	require.NoError(b, err)
	defer vm.Close()

	script := `
		function test(arg)
			return arg
		end
		return test
		`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := vm.CompileFunction("test", script)
		if err != nil {
			b.Fatal(err)
		}
		// Clean up the compiled function to avoid affecting subsequent iterations
		delete(vm.funcs, "test")
	}
}

// BenchmarkExecuteSimple benchmarks the execution of a simple Lua function.
func BenchmarkExecuteSimple(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	require.NoError(b, err)
	defer vm.Close()

	script := `
		function test(arg)
			return arg
		end
		return test
		`

	err = vm.CompileFunction("test", script)
	require.NoError(b, err)

	arg := lua.LString("benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := vm.Execute(context.Background(), "test", arg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExecuteComplex benchmarks the execution of a more complex Lua function with table manipulation.
func BenchmarkExecuteComplex(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	require.NoError(b, err)
	defer vm.Close()

	script := `
		function test(args)
			local sum = 0
			for i = 1, args.count do
				sum = sum + i
			end
			return sum
		end
		return test
		`

	err = vm.CompileFunction("test", script)
	require.NoError(b, err)

	tbl := lua.LTable{}
	tbl.RawSetString("count", lua.LNumber(100))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := vm.Execute(context.Background(), "test", &tbl)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExecuteWithLibrary benchmarks the execution of a Lua function that uses a preloaded library.
func BenchmarkExecuteWithLibrary(b *testing.B) {
	logger := zap.NewNop()

	libSource := `
		local lib = {}
		function lib.double(n)
			return n * 2
		end
		return lib
		`

	vm, err := NewVM(logger, WithLibrary("testlib", libSource))
	require.NoError(b, err)
	defer vm.Close()

	script := `
		local lib = require("testlib")
		function test(arg)
			return lib.double(arg)
		end
		return test
		`

	err = vm.CompileFunction("test", script)
	require.NoError(b, err)

	arg := lua.LNumber(5)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := vm.Execute(context.Background(), "test", arg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGlobalStateSetup benchmarks the initialization of a VM with global state
func BenchmarkGlobalStateSetup(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		vm, err := NewVM(logger, WithGlobalValue("state", stateTable))
		if err != nil {
			b.Fatal(err)
		}
		vm.Close()
	}
}

// BenchmarkGlobalStateAccess benchmarks accessing and modifying global state
func BenchmarkGlobalStateAccess(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	vm, err := NewVM(logger, WithGlobalValue("state", stateTable))
	require.NoError(b, err)
	defer vm.Close()

	err = vm.CompileFunction("increment", `
        function increment()
            state.count = state.count + 1
            return state.count
        end
        return increment
    `)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := vm.Execute(context.Background(), "increment", lua.LNil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGlobalStateMultiFunction benchmarks multiple functions accessing shared state
func BenchmarkGlobalStateMultiFunction(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))
	stateTable.RawSetString("lastOp", lua.LString(""))

	vm, err := NewVM(logger, WithGlobalValue("state", stateTable))
	require.NoError(b, err)
	defer vm.Close()

	// Compile increment function
	err = vm.CompileFunction("increment", `
        function increment()
            state.count = state.count + 1
            state.lastOp = "increment"
            return state.count
        end
        return increment
    `)
	require.NoError(b, err)

	// Compile get status function
	err = vm.CompileFunction("getStatus", `
        function getStatus()
            return {count = state.count, lastOp = state.lastOp}
        end
        return getStatus
    `)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Increment
		_, err := vm.Execute(context.Background(), "increment", lua.LNil)
		if err != nil {
			b.Fatal(err)
		}
		// Get status
		_, err = vm.Execute(context.Background(), "getStatus", lua.LNil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGlobalStateDirectAccess benchmarks direct state access via State()
func BenchmarkGlobalStateDirectAccess(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	vm, err := NewVM(logger, WithGlobalValue("state", stateTable))
	require.NoError(b, err)
	defer vm.Close()

	L := vm.State()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		globalState := L.GetGlobal("state").(*lua.LTable)
		count := globalState.RawGetString("count").(lua.LNumber)
		globalState.RawSetString("count", lua.LNumber(count+1))
	}
}
