package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/yuin/gopher-lua/parse"

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
		`

		if err := vm.Import(script, "test", "test"); err != nil {
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

		if err := vm.Import(script, "test", "test"); err != nil {
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

		if err := vm.Import(script, "test"); err == nil {
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

		if err := vm.Import(script, "test", "different_name"); err != nil {
			t.Fatal(err)
		}

		if _, err := vm.Execute(context.Background(), "different_name"); err != nil {
			t.Fatal(err)
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

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
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

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
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

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
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

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
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
		`

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		tbl := &lua.LTable{}
		tbl.RawSetString("message", lua.LString("hello"))

		result, err := vm.Execute(context.Background(), "test", tbl)
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
		vm, err := NewVM(logger, WithGlobalFunction("test", func(l *lua.LState) int {
			l.Push(lua.LString("global"))
			return 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.DoString(context.Background(), `assert(test() == "global")`, "test"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with global value", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalValue("TEST_VALUE", lua.LString("test")))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.DoString(context.Background(), `assert(TEST_VALUE == "test")`, "test"); err != nil {
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

		if err := vm.DoString(context.Background(), `
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

		if err := vm.Import("this is not valid lua", "invalid"); err == nil {
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
		`

		if err := vm.Import(script, "test", "test"); err != nil {
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

	// Set up an initial global State
	if err := vm.DoString(context.Background(), `
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

	// The first increment should return 1
	if err := vm.DoString(context.Background(), `assert(increment() == 1)`, "test1"); err != nil {
		t.Fatal(err)
	}

	// The second increment should return 2
	if err := vm.DoString(context.Background(), `assert(increment() == 2)`, "test2"); err != nil {
		t.Fatal(err)
	}

	// GetField count should return the current value (2)
	if err := vm.DoString(context.Background(), `assert(getCount() == 2)`, "test3"); err != nil {
		t.Fatal(err)
	}

	// Verify State persists even with new chunk execution
	if err := vm.DoString(context.Background(), `assert(State.count == 2)`, "test4"); err != nil {
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

	// StartString increment function
	if err := vm.Import(`
		function increment()
			State.count = State.count + 1
			return State.count
		end
	`, "increment", "increment"); err != nil {
		t.Fatal(err)
	}

	// StartString getCount function
	if err := vm.Import(`
		function getCount()
			return State.count
		end
	`, "getCount", "getCount"); err != nil {
		t.Fatal(err)
	}

	// First increment should return 1
	result, err := vm.Execute(context.Background(), "increment")
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(1) {
		t.Errorf("got %v, want %v", result, lua.LNumber(1))
	}

	// Second increment should return 2
	result, err = vm.Execute(context.Background(), "increment")
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(2) {
		t.Errorf("got %v, want %v", result, lua.LNumber(2))
	}

	// GetCount should return current value (2)
	result, err = vm.Execute(context.Background(), "getCount")
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(2) {
		t.Errorf("got %v, want %v", result, lua.LNumber(2))
	}

	// Verify final State directly through Lua State
	globalState := vm.state.GetGlobal("State").(*lua.LTable)
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

	t.Run("error in AddTaskQueue with traceback", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
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
		if err := vm.Import(`
			function test()
				local x = nil
				return x.nonexistent
			end
		`, "bad_function", "test"); err != nil {
			t.Fatal(err)
		}

		_, err := vm.Execute(context.Background(), "test")
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
		err := vm.Import(`
			function test()
				if true then
					print("missing end")
			return test
		`, "syntax_error", "test")
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

func TestVM_SecurityRestrictions(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	t.Run("os library should not be available", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
			if os then
				error("os library should not be available")
			end
		`, "os_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require os
		err = vm.DoString(context.Background(), `require("os")`, "os_require_test")
		if err == nil {
			t.Error("expected error when requiring os library")
		}

		// Verify specific os operations fail
		err = vm.DoString(context.Background(), `os.execute("echo test")`, "os_execute_test")
		if err == nil {
			t.Error("os.execute should not be available")
		}
	})

	t.Run("io library should not be available", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
			if io then
				error("io library should not be available")
			end
		`, "io_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require io
		err = vm.DoString(context.Background(), `require("io")`, "io_require_test")
		if err == nil {
			t.Error("expected error when requiring io library")
		}

		// Verify specific io operations fail
		err = vm.DoString(context.Background(), `io.open("test.txt", "r")`, "io_open_test")
		if err == nil {
			t.Error("io.open should not be available")
		}
	})

	t.Run("loadlib operations should not work", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
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
		err = vm.DoString(context.Background(), `
			local t = require("testlib")
			assert(t.test() == "ok")
		`, "require_test")
		if err != nil {
			t.Fatal(err)
		}

		// Try to require a non-existent module
		err = vm.DoString(context.Background(), `require("nonexistent")`, "require_nonexistent_test")
		if err == nil {
			t.Error("expected error when requiring non-existent module")
		}

		// Try to require a system module
		err = vm.DoString(context.Background(), `require("socket")`, "require_system_test")
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
				err := vm.DoString(context.Background(), tt.script, tt.name)
				if err == nil {
					t.Errorf("%s: expected error but got none", tt.name)
				}
			})
		}
	})
}

func TestVM_Mount(t *testing.T) {
	logger := zap.NewNop()

	t.Run("share bytecode between VMs", func(t *testing.T) {
		// Create first VM and compile function
		vm1, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm1.Close()

		script := `
		function test(arg)
			return "Hello " .. arg
		end
		`

		// Parse and compile the script to get function prototype
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function in first VM
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Create second VM
		vm2, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm2.Close()

		// Mount same function prototype in second VM
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test execution in both VMs
		arg := lua.LString("World")
		expected := lua.LString("Hello World")

		// Run in VM1
		result1, err := vm1.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result1 != expected {
			t.Errorf("VM1: got %v, want %v", result1, expected)
		}

		// Run in VM2
		result2, err := vm2.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result2 != expected {
			t.Errorf("VM2: got %v, want %v", result2, expected)
		}
	})

	t.Run("mount function with state isolation", func(t *testing.T) {
		// Create two VMs
		vm1, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm1.Close()

		vm2, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm2.Close()

		script := `
		local count = 0
		function test()
			count = count + 1
			return count
		end
		`

		// Parse and compile
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Run in VM1 twice
		result1, err := vm1.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LNumber(1) {
			t.Errorf("VM1 first call: got %v, want %v", result1, lua.LNumber(1))
		}

		result1, err = vm1.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LNumber(2) {
			t.Errorf("VM1 second call: got %v, want %v", result1, lua.LNumber(2))
		}

		// Run in VM2 should start from 1 again
		result2, err := vm2.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result2 != lua.LNumber(1) {
			t.Errorf("VM2 first call: got %v, want %v", result2, lua.LNumber(1))
		}
	})

	t.Run("mount module style function", func(t *testing.T) {
		vm1, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm1.Close()

		vm2, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm2.Close()

		script := `
		local M = {}
		function M.test(arg)
			return "Module " .. arg
		end
		return M
		`

		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		arg := lua.LString("Call")
		expected := lua.LString("Module Call")

		// Test in both VMs
		result1, err := vm1.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result1 != expected {
			t.Errorf("VM1: got %v, want %v", result1, expected)
		}

		result2, err := vm2.Execute(context.Background(), "test", arg)
		if err != nil {
			t.Fatal(err)
		}
		if result2 != expected {
			t.Errorf("VM2: got %v, want %v", result2, expected)
		}
	})
}

func TestVM_FunctionNameDetection(t *testing.T) {
	logger := zap.NewNop()

	t.Run("direct function return", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Test multiple function returns
		cases := []struct {
			name   string
			script string
			arg    lua.LValue
			want   lua.LValue
		}{
			{
				name: "simple_return",
				script: `
					function simple_return(x)
						return x * 2
					end
					return simple_return
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(10),
			},
			{
				name: "anonymous_return",
				script: `
					return function(x)
						return x * 3
					end
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(15),
			},
			{
				name: "local_with_return",
				script: `
					local function local_func(x)
						return x * 4
					end
					return local_func
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(20),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := vm.Import(tc.script, tc.name, tc.name); err != nil {
					t.Fatal(err)
				}

				result, err := vm.Execute(context.Background(), tc.name, tc.arg)
				if err != nil {
					t.Fatal(err)
				}
				if result != tc.want {
					t.Errorf("got %v, want %v", result, tc.want)
				}
			})
		}
	})

	t.Run("module table return", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		cases := []struct {
			name   string
			script string
			arg    lua.LValue
			want   lua.LValue
		}{
			{
				name: "basic_module",
				script: `
					local M = {}
					function M.basic_module(x)
						return x .. " processed"
					end
					return M
				`,
				arg:  lua.LString("test"),
				want: lua.LString("test processed"),
			},
			{
				name: "nested_module",
				script: `
					local M = {
						utils = {}
					}
					function M.utils.nested_module(x)
						return x * 5
					end
					return M.utils
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(25),
			},
			{
				name: "complex_module",
				script: `
					local M = {}
					local function helper(x)
						return x + 1
					end
					function M.complex_module(x)
						return helper(x)
					end
					return M
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(6),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := vm.Import(tc.script, tc.name, tc.name); err != nil {
					t.Fatal(err)
				}

				result, err := vm.Execute(context.Background(), tc.name, tc.arg)
				if err != nil {
					t.Fatal(err)
				}
				if result != tc.want {
					t.Errorf("got %v, want %v", result, tc.want)
				}
			})
		}
	})

	t.Run("global function", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		cases := []struct {
			name   string
			script string
			arg    lua.LValue
			want   lua.LValue
		}{
			{
				name: "simple_global",
				script: `
					function simple_global(x)
						return x * 2
					end
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(10),
			},
			{
				name: "global_with_local",
				script: `
					local helper = function(x)
						return x + 1
					end
					function global_with_local(x)
						return helper(x)
					end
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(6),
			},
			{
				name: "global_complex",
				script: `
					local state = 0
					function global_complex(x)
						state = state + x
						return state
					end
				`,
				arg:  lua.LNumber(5),
				want: lua.LNumber(5),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := vm.Import(tc.script, tc.name, tc.name); err != nil {
					t.Fatal(err)
				}

				result, err := vm.Execute(context.Background(), tc.name, tc.arg)
				if err != nil {
					t.Fatal(err)
				}
				if result != tc.want {
					t.Errorf("got %v, want %v", result, tc.want)
				}
			})
		}
	})

	t.Run("function name mismatches", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		cases := []struct {
			name   string
			script string
		}{
			{
				name: "wrong_global_name",
				script: `
					function wrong_name()
						return true
					end
				`,
			},
			{
				name: "wrong_module_name",
				script: `
					local M = {}
					function M.wrong_name()
						return true
					end
					return M
				`,
			},
			{
				name: "no_function_return",
				script: `
					return "not a function"
				`,
			},
			{
				name: "empty_module",
				script: `
					local M = {}
					return M
				`,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := vm.Import(tc.script, tc.name, tc.name)
				if err == nil {
					t.Error("expected error for mismatched function name, got nil")
				}
			})
		}
	})

	t.Run("function resolution precedence", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			-- Global function
			function test()
				return "global"
			end

			-- Return a different function that should take precedence
			return function()
				return "direct"
			end
		`

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		if result != lua.LString("direct") {
			t.Errorf("got %v, want %v", result, lua.LString("direct"))
		}
	})
}

func TestVM_SharedLibraryProto(t *testing.T) {
	logger := zap.NewNop()

	// First compile the library
	libSource := `
        local lib = {}
        function lib.add(a, b)
            return a + b
        end
        function lib.multiply(a, b)
            return a * b
        end
        return lib
    `

	// Parse and compile the library to get function prototype
	chunk, err := parse.Parse(strings.NewReader(libSource), "mathlib")
	if err != nil {
		t.Fatal(err)
	}

	proto, err := lua.Compile(chunk, "mathlib")
	if err != nil {
		t.Fatal(err)
	}

	// Create two VMs sharing the same library prototype
	vm1, err := NewVM(logger, WithLibrary("mathlib", proto))
	if err != nil {
		t.Fatal(err)
	}
	defer vm1.Close()

	vm2, err := NewVM(logger, WithLibrary("mathlib", proto))
	if err != nil {
		t.Fatal(err)
	}
	defer vm2.Close()

	// Test code that uses the library
	testScript := `
        local math = require("mathlib")
        local sum = math.add(5, 3)
        local product = math.multiply(4, 6)
        return sum, product
    `

	// Test VM1
	err = vm1.DoString(context.Background(), testScript, "test1")
	if err != nil {
		t.Fatal("VM1 failed:", err)
	}

	// Test VM2 with different values
	err = vm2.DoString(context.Background(), testScript, "test2")
	if err != nil {
		t.Fatal("VM2 failed:", err)
	}

	// Test specific function calls through Lua
	t.Run("specific function calls", func(t *testing.T) {
		script := `
            local math = require("mathlib")
            return math.add(10, 20)
        `

		// Test on VM1
		err := vm1.DoString(context.Background(), script, "vm1_test")
		if err != nil {
			t.Fatal("VM1 specific call failed:", err)
		}

		// Test on VM2
		err = vm2.DoString(context.Background(), script, "vm2_test")
		if err != nil {
			t.Fatal("VM2 specific call failed:", err)
		}
	})

	// Test state isolation
	t.Run("state isolation", func(t *testing.T) {
		stateScript := `
            local math = require("mathlib")
            if not _G.counter then
                _G.counter = 0
            end
            _G.counter = _G.counter + 1
            return _G.counter
        `

		// Run on VM1 twice
		err := vm1.DoString(context.Background(), stateScript, "vm1_state")
		if err != nil {
			t.Fatal("VM1 state test failed:", err)
		}
		err = vm1.DoString(context.Background(), stateScript, "vm1_state")
		if err != nil {
			t.Fatal("VM1 second state test failed:", err)
		}

		// Run on VM2 - should start from 0
		err = vm2.DoString(context.Background(), stateScript, "vm2_state")
		if err != nil {
			t.Fatal("VM2 state test failed:", err)
		}
	})
}

func TestVM_Import_EdgeCases(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	t.Run("empty function name", func(t *testing.T) {
		script := `
			function test()
				return 1
			end
		`
		if err := vm.Import(script, "test", ""); err == nil {
			t.Error("expected error with empty function name, got nil")
		}
	})

	t.Run("invalid function name", func(t *testing.T) {
		script := `
            function invalid$name()
                return 1
            end
        `
		if err := vm.Import(script, "test", "invalid$name"); err == nil {
			t.Error("expected error with invalid function name, got nil")
		}
	})

	t.Run("function not found in prototype", func(t *testing.T) {
		script := `
            function test()
                return 1
            end
        `
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.Mount(proto, "nonexistent"); err == nil {
			t.Error("expected error with non-existent function name, got nil")
		}
	})
}

func TestVM_Execute_ArgumentErrors(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	script := `
		function test(a, b)
			return a + b
		end
	`
	if err := vm.Import(script, "test", "test"); err != nil {
		t.Fatal(err)
	}

	t.Run("too few arguments", func(t *testing.T) {
		if _, err := vm.Execute(context.Background(), "test", lua.LNumber(1)); err == nil {
			t.Error("expected error with too few arguments, got nil")
		}
	})

	t.Run("incorrect argument type", func(t *testing.T) {
		if _, err := vm.Execute(context.Background(), "test", lua.LString("a"), lua.LNumber(2)); err == nil {
			t.Error("expected error with incorrect argument type, got nil")
		}
	})
}

func TestVM_Context_Cancellation(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	script := `
		function test()
			while true do
			end
		end
	`
	if err := vm.Import(script, "test", "test"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = vm.Execute(ctx, "test")
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	} else if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("expected error message to contain 'context canceled' or 'interrupted', got: %v", err)
	}
}
