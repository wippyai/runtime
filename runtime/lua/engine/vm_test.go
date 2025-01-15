package engine

import (
	"context"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create new VM", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		if vm == nil {
			t.Fatal("expected non-nil VM")
		}
		defer vm.Close()
	})

	t.Run("compile and execute simple function", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(arg)
			return arg
		end
		return test
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		arg := lua.LString("hello world")
		result, err := vm.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result != arg {
			t.Errorf("got %v, want %v", result, arg)
		}
	})

	t.Run("compile and execute simple function by name", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(arg)
			return arg
		end
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		arg := lua.LString("hello world")
		result, err := vm.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result != arg {
			t.Errorf("got %v, want %v", result, arg)
		}
	})

	t.Run("compile no func", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := ``

		if err := vm.CompileFunction("test", script); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("global function without matching name", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function different_name()
			return "hello world"
		end
		`

		if err := vm.CompileFunction("test", script); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("local function with return", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		local function test()
			return "hello world"
		end
		return test
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("hello world") {
			t.Errorf("got %v, want %v", result, lua.LString("hello world"))
		}
	})

	t.Run("module style declaration", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		local M = {}
		function M.test()
			return "hello world"
		end
		return M
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("hello world") {
			t.Errorf("got %v, want %v", result, lua.LString("hello world"))
		}
	})

	t.Run("anonymous function assignment", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		test = function()
			return "hello world"
		end
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("hello world") {
			t.Errorf("got %v, want %v", result, lua.LString("hello world"))
		}
	})

	t.Run("mixed global and local declarations", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		local function helper()
			return "helper"
		end
		
		function test()
			return helper()
		end
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test", lua.LNil)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("helper") {
			t.Errorf("got %v, want %v", result, lua.LString("helper"))
		}
	})

	t.Run("execute function with table argument", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(args)
			return args.message
		end
		return test
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		tbl := lua.LTable{}
		tbl.RawSetString("message", lua.LString("hello"))

		result, err := vm.Execute(context.Background(), "test", &tbl)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("hello") {
			t.Errorf("got %v, want %v", result, lua.LString("hello"))
		}
	})
}

func TestVM_Options(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with global function", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalFunction("test", func(L *lua.LState) int {
			L.Push(lua.LString("global"))
			return 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.DoString(nil, `assert(test() == "global")`, "test"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with global value", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalValue("TEST_VALUE", lua.LString("test")))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.DoString(nil, `assert(TEST_VALUE == "test")`, "test"); err != nil {
			t.Fatal(err)
		}
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
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.DoString(nil, `
		local lib = require("testlib")
		assert(lib.test() == "library")
		`, "test"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestVM_Errors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("execute non-existent function", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if _, err := vm.Execute(context.Background(), "nonexistent", lua.LNil); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("compile invalid script", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.CompileFunction("invalid", "this is not valid lua"); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("execute script with runtime error", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test()
			error("runtime error")
		end
		return test
		`

		if err := vm.CompileFunction("test", script); err != nil {
			t.Fatal(err)
		}

		if _, err := vm.Execute(context.Background(), "test", lua.LNil); err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestVM_GlobalState(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	// Set up initial global State
	if err := vm.DoString(nil, `
		State = {count = 0}
		function increment()
			State.count = State.count + 1
			return State.count
		end
		function getCount()
			return State.count
		end
	`, "setup"); err != nil {
		t.Fatal(err)
	}

	// First increment should return 1
	if err := vm.DoString(nil, `assert(increment() == 1)`, "test1"); err != nil {
		t.Fatal(err)
	}

	// Second increment should return 2
	if err := vm.DoString(nil, `assert(increment() == 2)`, "test2"); err != nil {
		t.Fatal(err)
	}

	// Get count should return the current value (2)
	if err := vm.DoString(nil, `assert(getCount() == 2)`, "test3"); err != nil {
		t.Fatal(err)
	}

	// Verify State persists even with new chunk execution
	if err := vm.DoString(nil, `assert(State.count == 2)`, "test4"); err != nil {
		t.Fatal(err)
	}
}

func TestVM_CompiledGlobalState(t *testing.T) {
	logger := zap.NewNop()

	// Create initial State table
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	// Create VM with global State
	vm, err := NewVM(logger, WithGlobalValue("State", stateTable))
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	// Compile increment function
	if err := vm.CompileFunction("increment", `
		function increment()
			State.count = State.count + 1
			return State.count
		end
		return increment
	`); err != nil {
		t.Fatal(err)
	}

	// Compile getCount function
	if err := vm.CompileFunction("getCount", `
		function getCount()
			return State.count
		end
		return getCount
	`); err != nil {
		t.Fatal(err)
	}

	// First increment should return 1
	result, err := vm.Execute(context.Background(), "increment", lua.LNil)
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(1) {
		t.Errorf("got %v, want %v", result, lua.LNumber(1))
	}

	// Second increment should return 2
	result, err = vm.Execute(context.Background(), "increment", lua.LNil)
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(2) {
		t.Errorf("got %v, want %v", result, lua.LNumber(2))
	}

	// GetCount should return current value (2)
	result, err = vm.Execute(context.Background(), "getCount", lua.LNil)
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(2) {
		t.Errorf("got %v, want %v", result, lua.LNumber(2))
	}

	// Verify final State directly through Lua State
	globalState := vm.State().GetGlobal("State").(*lua.LTable)
	count := globalState.RawGetString("count")
	if count != lua.LNumber(2) {
		t.Errorf("got %v, want %v", count, lua.LNumber(2))
	}
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
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	t.Run("error in Init with traceback", func(t *testing.T) {
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

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		errStr := err.Error()
		// Check error content
		if !strings.Contains(errStr, "deep error") {
			t.Error("error should contain 'deep error'")
		}
		// Check function names in trace
		if !strings.Contains(errStr, "in function 'deep'") {
			t.Error("error should contain function 'deep' in trace")
		}
		if !strings.Contains(errStr, "in function 'middle'") {
			t.Error("error should contain function 'middle' in trace")
		}
		if !strings.Contains(errStr, "in function 'top'") {
			t.Error("error should contain function 'top' in trace")
		}
		// Check script name and line numbers
		if !strings.Contains(errStr, "<error_test>:3:") {
			t.Error("error should contain line reference to error_test:3")
		}
		if !strings.Contains(errStr, "<error_test>:6:") {
			t.Error("error should contain line reference to error_test:6")
		}
		if !strings.Contains(errStr, "<error_test>:9:") {
			t.Error("error should contain line reference to error_test:9")
		}
	})

	t.Run("error in compiled function", func(t *testing.T) {
		if err := vm.CompileFunction("bad_function", `
			function test()
				local x = nil
				return x.nonexistent
			end
			return test
		`); err != nil {
			t.Fatal(err)
		}

		_, err := vm.Execute(context.Background(), "bad_function", lua.LNil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		errStr := err.Error()
		// Check exact error message
		if !strings.Contains(errStr, "attempt to index a non-table object(nil)") {
			t.Error("error should mention attempt to index nil")
		}
		if !strings.Contains(errStr, "bad_function:4:") {
			t.Error("error should contain line reference bad_function:4")
		}
	})

	t.Run("syntax error in compile", func(t *testing.T) {
		err := vm.CompileFunction("syntax_error", `
			function test()
				if true then
					print("missing end")
			return test
		`)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		errStr := err.Error()
		if !strings.Contains(errStr, "syntax error") {
			t.Error("error should mention syntax error")
		}
		if !strings.Contains(errStr, "syntax_error") {
			t.Error("error should reference syntax_error")
		}
	})
}

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

func BenchmarkCompileFunction(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		b.Fatal(err)
	}
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
		if err := vm.CompileFunction("test", script); err != nil {
			b.Fatal(err)
		}
		// Clean up the compiled function to avoid affecting subsequent iterations
		delete(vm.funcs, "test")
	}
}

func BenchmarkExecuteSimple(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	script := `
		function test(arg)
			return arg
		end
		return test
	`

	if err := vm.CompileFunction("test", script); err != nil {
		b.Fatal(err)
	}

	arg := lua.LString("benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := vm.Execute(context.Background(), "test", arg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteComplex(b *testing.B) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		b.Fatal(err)
	}
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

	if err := vm.CompileFunction("test", script); err != nil {
		b.Fatal(err)
	}

	tbl := lua.LTable{}
	tbl.RawSetString("count", lua.LNumber(100))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := vm.Execute(context.Background(), "test", &tbl); err != nil {
			b.Fatal(err)
		}
	}
}

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
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	script := `
		local lib = require("testlib")
		function test(arg)
			return lib.double(arg)
		end
		return test
	`

	if err := vm.CompileFunction("test", script); err != nil {
		b.Fatal(err)
	}

	arg := lua.LNumber(5)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := vm.Execute(context.Background(), "test", arg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGlobalStateSetup(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		vm, err := NewVM(logger, WithGlobalValue("State", stateTable))
		if err != nil {
			b.Fatal(err)
		}
		vm.Close()
	}
}

func BenchmarkGlobalStateAccess(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	vm, err := NewVM(logger, WithGlobalValue("State", stateTable))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	if err := vm.CompileFunction("increment", `
		function increment()
			State.count = State.count + 1
			return State.count
		end
		return increment
	`); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := vm.Execute(context.Background(), "increment", lua.LNil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGlobalStateMultiFunction(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))
	stateTable.RawSetString("lastOp", lua.LString(""))

	vm, err := NewVM(logger, WithGlobalValue("State", stateTable))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	if err := vm.CompileFunction("increment", `
		function increment()
			State.count = State.count + 1
			State.lastOp = "increment"
			return State.count
		end
		return increment
	`); err != nil {
		b.Fatal(err)
	}

	if err := vm.CompileFunction("getStatus", `
		function getStatus()
			return {count = State.count, lastOp = State.lastOp}
		end
		return getStatus
	`); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Increment
		if _, err := vm.Execute(context.Background(), "increment", lua.LNil); err != nil {
			b.Fatal(err)
		}
		// Get status
		if _, err := vm.Execute(context.Background(), "getStatus", lua.LNil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGlobalStateDirectAccess(b *testing.B) {
	logger := zap.NewNop()
	stateTable := &lua.LTable{}
	stateTable.RawSetString("count", lua.LNumber(0))

	vm, err := NewVM(logger, WithGlobalValue("State", stateTable))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	L := vm.State()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		globalState := L.GetGlobal("State").(*lua.LTable)
		count := globalState.RawGetString("count").(lua.LNumber)
		globalState.RawSetString("count", lua.LNumber(count+1))
	}
}

func TestVM_SecurityRestrictions(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	t.Run("os library should not be available", func(t *testing.T) {
		err := vm.DoString(nil, `
			if os then
				error("os library should not be available")
			end
		`, "os_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require os
		err = vm.DoString(nil, `require("os")`, "os_require_test")
		if err == nil {
			t.Error("expected error when requiring os library")
		}

		// Verify specific os operations fail
		err = vm.DoString(nil, `os.execute("echo test")`, "os_execute_test")
		if err == nil {
			t.Error("os.execute should not be available")
		}
	})

	t.Run("io library should not be available", func(t *testing.T) {
		err := vm.DoString(nil, `
			if io then
				error("io library should not be available")
			end
		`, "io_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require io
		err = vm.DoString(nil, `require("io")`, "io_require_test")
		if err == nil {
			t.Error("expected error when requiring io library")
		}

		// Verify specific io operations fail
		err = vm.DoString(nil, `io.open("test.txt", "r")`, "io_open_test")
		if err == nil {
			t.Error("io.open should not be available")
		}
	})

	t.Run("loadlib operations should not work", func(t *testing.T) {
		err := vm.DoString(nil, `
			-- Try to load a C library (should fail)
			local ok, err = package.loadlib("test.so", "luaopen_test")
			if ok then
				error("should not be able to load C libraries")
			end
		`, "loadlib_test")
		if err == nil {
			t.Error("expected error when trying to load C library")
		}
	})

	t.Run("require should only work with preloaded modules", func(t *testing.T) {
		// Add a test library
		testLib := `
			local lib = {}
			function lib.test() return "ok" end
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("testlib", testLib))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Verify we can load our preloaded module
		err = vm.DoString(nil, `
			local t = require("testlib")
			assert(t.test() == "ok")
		`, "require_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require a non-existent module
		err = vm.DoString(nil, `require("nonexistent")`, "require_nonexistent_test")
		if err == nil {
			t.Error("expected error when requiring non-existent module")
		}

		// Try to require a system module
		err = vm.DoString(nil, `require("socket")`, "require_system_test")
		if err == nil {
			t.Error("expected error when requiring system module")
		}
	})

	t.Run("attempt file system operations should fail", func(t *testing.T) {
		tests := []struct {
			name   string
			script string
		}{
			{"write file attempt", `io.output("test.txt")`},
			{"read file attempt", `io.input("test.txt")`},
			{"direct file open", `io.open("test.txt", "r")`},
			{"os execute", `os.execute("ls")`},
			{"os remove", `os.remove("test.txt")`},
			{"os rename", `os.rename("a.txt", "b.txt")`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := vm.DoString(nil, tt.script, tt.name)
				if err == nil {
					t.Errorf("%s: expected error but got none", tt.name)
				}
			})
		}
	})
}
