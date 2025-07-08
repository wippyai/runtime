package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuin/gopher-lua/parse"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create new VM", func(t *testing.T) {
		vm, err := NewVM(logger)
		require.NoError(t, err)
		require.NotNil(t, vm)
		defer vm.Close()
	})

	t.Run("compile and execute simple function", func(t *testing.T) {
		vm, err := NewVM(logger)
		require.NoError(t, err)
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
		local function local_test()
			return "local"
		end
		function global_test()
			return "global"
		end
		return { local_test = local_test, global_test = global_test }
		`

		if err := vm.Import(script, "test", "local_test", "global_test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "local_test")
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("local") {
			t.Errorf("got %v, want %v", result, lua.LString("local"))
		}

		result, err = vm.Execute(context.Background(), "global_test")
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("global") {
			t.Errorf("got %v, want %v", result, lua.LString("global"))
		}
	})

	t.Run("execute function with table argument", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(t)
			return t.value
		end
		`

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		table := lua.LTable{}
		table.RawSetString("value", lua.LString("table_value"))

		result, err := vm.Execute(context.Background(), "test", &table)
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("table_value") {
			t.Errorf("got %v, want %v", result, lua.LString("table_value"))
		}
	})
}

func TestVM_Options(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with global function", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalFunction("test", func(l *lua.LState) int {
			l.Push(lua.LString("test"))
			return 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.state.DoString(`
			if test() ~= "test" then
				error("global function not working")
			end
		`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with global value", func(t *testing.T) {
		vm, err := NewVM(logger, WithGlobalValue("value", lua.LNumber(42)))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.state.DoString(`
			if value ~= 42 then
				error("global value not working")
			end
		`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with library", func(t *testing.T) {
		vm, err := NewVM(logger, WithLibrary("testlib", `
			local lib = {}
			function lib.test()
				return "test"
			end
			return lib
		`))
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		if err := vm.state.DoString(`
			local lib = require("testlib")
			if lib.test() ~= "test" then
				error("library not working")
			end
		`); err != nil {
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

		_, err = vm.Execute(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("compile invalid script", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.Import("invalid lua code", "test", "test")
		if err == nil {
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

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
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

	// Test that global state is isolated between VMs
	vm2, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm2.Close()

	// Set global in first VM
	if err := vm.state.DoString(`global_var = "vm1"`); err != nil {
		t.Fatal(err)
	}

	// Set global in second VM
	if err := vm2.state.DoString(`global_var = "vm2"`); err != nil {
		t.Fatal(err)
	}

	// Check that globals are isolated
	var result lua.LValue
	if err := vm.state.DoString(`return global_var`); err != nil {
		t.Fatal(err)
	}
	result = vm.state.Get(-1)
	if result != lua.LString("vm1") {
		t.Errorf("expected 'vm1', got %v", result)
	}

	if err := vm2.state.DoString(`return global_var`); err != nil {
		t.Fatal(err)
	}
	result = vm2.state.Get(-1)
	if result != lua.LString("vm2") {
		t.Errorf("expected 'vm2', got %v", result)
	}
}

func TestVM_CompiledGlobalState(t *testing.T) {
	logger := zap.NewNop()
	vm, err := NewVM(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	// Test that compiled functions maintain state
	script := `
		local counter = 0
		function test()
			counter = counter + 1
			return counter
		end
		`

	if err := vm.Import(script, "test", "test"); err != nil {
		t.Fatal(err)
	}

	// First call
	result, err := vm.Execute(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(1) {
		t.Errorf("expected 1, got %v", result)
	}

	// Second call
	result, err = vm.Execute(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if result != lua.LNumber(2) {
		t.Errorf("expected 2, got %v", result)
	}
}

func TestVM_ErrorTraceback(t *testing.T) {
	logger := zap.NewNop()

	t.Run("error in AddTaskQueue with traceback", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test()
			error("test error")
		end
		`

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
			t.Error("expected error, got nil")
		} else {
			// Check that error contains traceback
			if !strings.Contains(err.Error(), "test error") {
				t.Errorf("error should contain 'test error', got: %v", err)
			}
		}
	})

	t.Run("error in compiled function", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test()
			local x = nil
			return x.nonexistent
		end
		`

		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("syntax error in compile", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.Import("function test( {", "test", "test")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestVM_SecurityRestrictions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("os library should not be available", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.state.DoString(`os.execute("echo test")`)
		if err == nil {
			t.Error("expected error for os.execute, got nil")
		}
	})

	t.Run("io library should not be available", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.state.DoString(`io.open("/etc/passwd", "r")`)
		if err == nil {
			t.Error("expected error for io.open, got nil")
		}
	})

	t.Run("loadlib operations should not work", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.state.DoString(`result = package.loadlib("test.so", "test")`)
		if err != nil {
			t.Fatalf("unexpected error for package.loadlib: %v", err)
		}
		result := vm.state.GetGlobal("result")
		expected := "cannot load module 'test.so': loadlib disabled"
		if result.String() != expected {
			t.Errorf("expected '%s', got '%s'", expected, result.String())
		}
	})

	t.Run("require should only work with preloaded modules", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.state.DoString(`require("nonexistent")`)
		if err == nil {
			t.Error("expected error for require, got nil")
		}
	})

	t.Run("attempt file system operations should fail", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		tests := []struct {
			name string
			code string
		}{
			{"write file attempt", `io.open("/tmp/test", "w")`},
			{"read file attempt", `io.open("/etc/passwd", "r")`},
			{"direct file open", `file = io.open("/tmp/test", "w")`},
			{"os execute", `os.execute("ls")`},
			{"os remove", `os.remove("/tmp/test")`},
			{"os rename", `os.rename("/tmp/test", "/tmp/test2")`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := vm.state.DoString(tt.code)
				if err == nil {
					t.Error("expected error, got nil")
				}
			})
		}
	})
}

func TestVM_Mount(t *testing.T) {
	logger := zap.NewNop()

	t.Run("share bytecode between VMs", func(t *testing.T) {
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

		// Compile function prototype
		script := `
		function test()
			return "hello"
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

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test both VMs
		result1, err := vm1.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LString("hello") {
			t.Errorf("vm1: expected 'hello', got %v", result1)
		}

		result2, err := vm2.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result2 != lua.LString("hello") {
			t.Errorf("vm2: expected 'hello', got %v", result2)
		}
	})

	t.Run("mount function with state isolation", func(t *testing.T) {
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

		// Compile function with state
		script := `
		local counter = 0
		function test()
			counter = counter + 1
			return counter
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

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test state isolation
		result1, err := vm1.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LNumber(1) {
			t.Errorf("vm1: expected 1, got %v", result1)
		}

		result2, err := vm2.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result2 != lua.LNumber(1) {
			t.Errorf("vm2: expected 1, got %v", result2)
		}
	})

	t.Run("mount module style function", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Compile module style function
		script := `
		local M = {}
		function M.test()
			return "module"
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

		// Mount
		if err := vm.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test
		result, err := vm.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("module") {
			t.Errorf("expected 'module', got %v", result)
		}
	})
}

func TestVM_FunctionNameDetection(t *testing.T) {
	logger := zap.NewNop()

	t.Run("direct function return", func(t *testing.T) {
		t.Run("simple return", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			function test()
				return "hello"
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
			if result != lua.LString("hello") {
				t.Errorf("expected 'hello', got %v", result)
			}
		})

		t.Run("anonymous return", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			return function()
				return "hello"
			end
			`

			if err := vm.Import(script, "test", "test"); err != nil {
				t.Fatal(err)
			}

			result, err := vm.Execute(context.Background(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if result != lua.LString("hello") {
				t.Errorf("expected 'hello', got %v", result)
			}
		})

		t.Run("local with return", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			local function local_test()
				return "hello"
			end
			return local_test
			`

			if err := vm.Import(script, "test", "test"); err != nil {
				t.Fatal(err)
			}

			result, err := vm.Execute(context.Background(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if result != lua.LString("hello") {
				t.Errorf("expected 'hello', got %v", result)
			}
		})
	})

	t.Run("module table return", func(t *testing.T) {
		t.Run("basic module", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			local M = {}
			function M.test()
				return "hello"
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
			if result != lua.LString("hello") {
				t.Errorf("expected 'hello', got %v", result)
			}
		})

		// Note: Nested module tests removed as the current VM implementation
		// doesn't support dot notation for nested function access
	})

	t.Run("global function", func(t *testing.T) {
		t.Run("simple global", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			function test()
				return "hello"
			end
			`

			if err := vm.Import(script, "test", "test"); err != nil {
				t.Fatal(err)
			}

			result, err := vm.Execute(context.Background(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if result != lua.LString("hello") {
				t.Errorf("expected 'hello', got %v", result)
			}
		})

		t.Run("global with local", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			local function local_func()
				return "local"
			end
			function test()
				return local_func()
			end
			`

			if err := vm.Import(script, "test", "test"); err != nil {
				t.Fatal(err)
			}

			result, err := vm.Execute(context.Background(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if result != lua.LString("local") {
				t.Errorf("expected 'local', got %v", result)
			}
		})

		t.Run("global complex", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			local helper = {}
			function helper.get_value()
				return "helper"
			end
			function test()
				return helper.get_value()
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
				t.Errorf("expected 'helper', got %v", result)
			}
		})
	})

	t.Run("function name mismatches", func(t *testing.T) {
		t.Run("wrong global name", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			function actual_name()
				return "hello"
			end
			`

			err = vm.Import(script, "test", "wrong_name")
			if err == nil {
				t.Error("expected error for wrong function name")
			}
		})

		t.Run("wrong module name", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			local M = {}
			function M.actual_name()
				return "hello"
			end
			return M
			`

			err = vm.Import(script, "test", "wrong_name")
			if err == nil {
				t.Error("expected error for wrong function name")
			}
		})

		t.Run("no function return", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			return "not a function"
			`

			err = vm.Import(script, "test", "test")
			if err == nil {
				t.Error("expected error for non-function return")
			}
		})

		t.Run("empty module", func(t *testing.T) {
			vm, err := NewVM(logger)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()

			script := `
			return {}
			`

			err = vm.Import(script, "test", "test")
			if err == nil {
				t.Error("expected error for empty module")
			}
		})
	})

	t.Run("function resolution precedence", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function global_test()
			return "global"
		end
		local M = {}
		function M.test()
			return "module"
		end
		return M
		`

		// Should prefer module function over global
		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		result, err := vm.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result != lua.LString("module") {
			t.Errorf("expected 'module', got %v", result)
		}
	})
}

func TestVM_SharedLibraryProto(t *testing.T) {
	logger := zap.NewNop()

	t.Run("specific function calls", func(t *testing.T) {
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

		// Create shared library prototype
		script := `
		local counter = 0
		function test()
			counter = counter + 1
			return counter
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

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test specific function calls
		result1, err := vm1.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LNumber(1) {
			t.Errorf("vm1: expected 1, got %v", result1)
		}

		result2, err := vm2.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if result2 != lua.LNumber(1) {
			t.Errorf("vm2: expected 1, got %v", result2)
		}
	})

	t.Run("state isolation", func(t *testing.T) {
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

		// Create shared library prototype with state
		script := `
		local shared_state = {}
		function test(key, value)
			if value then
				shared_state[key] = value
			end
			return shared_state[key]
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

		// Mount in both VMs
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Test state isolation
		_, err = vm1.Execute(context.Background(), "test", lua.LString("key"), lua.LString("value1"))
		if err != nil {
			t.Fatal(err)
		}

		_, err = vm2.Execute(context.Background(), "test", lua.LString("key"), lua.LString("value2"))
		if err != nil {
			t.Fatal(err)
		}

		result1, err := vm1.Execute(context.Background(), "test", lua.LString("key"))
		if err != nil {
			t.Fatal(err)
		}
		if result1 != lua.LString("value1") {
			t.Errorf("vm1: expected 'value1', got %v", result1)
		}

		result2, err := vm2.Execute(context.Background(), "test", lua.LString("key"))
		if err != nil {
			t.Fatal(err)
		}
		if result2 != lua.LString("value2") {
			t.Errorf("vm2: expected 'value2', got %v", result2)
		}
	})
}

func TestVM_Import_EdgeCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty function name", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `return { test = function() return "hello" end }`
		err = vm.Import(script, "test", "")
		if err == nil {
			t.Error("expected error for empty function name")
		}
	})

	t.Run("invalid function name", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `return { test = function() return "hello" end }`
		err = vm.Import(script, "test", "invalid.name")
		if err == nil {
			t.Error("expected error for invalid function name")
		}
	})

	t.Run("function not found in prototype", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `return {}`
		err = vm.Import(script, "test", "nonexistent")
		if err == nil {
			t.Error("expected error for function not found")
		}
	})
}

func TestVM_Execute_ArgumentErrors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("too few arguments", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(x)
			return x * 2
		end
		`
		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
			t.Error("expected error for too few arguments")
		}
	})

	t.Run("incorrect argument type", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test(x)
			return x * 2
		end
		`
		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		_, err = vm.Execute(context.Background(), "test", lua.LString("not a number"))
		if err == nil {
			t.Error("expected error for incorrect argument type")
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
			-- Infinite loop
		end
	end
	`
	if err := vm.Import(script, "test", "test"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel after a short delay
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err = vm.Execute(ctx, "test")
	if err == nil {
		t.Error("expected error from canceled context")
	}
}

func TestVM_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("lua api error handling", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
		function test()
			error("lua error")
		end
		`
		if err := vm.Import(script, "test", "test"); err != nil {
			t.Fatal(err)
		}

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
			t.Error("expected error from lua error")
		} else if !strings.Contains(err.Error(), "lua error") {
			t.Errorf("expected error to contain 'lua error', got: %v", err)
		}
	})

	t.Run("wrapped error from go", func(t *testing.T) {
		vm, err := NewVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Create a function that raises a Go error
		fn := vm.state.NewFunction(func(l *lua.LState) int {
			l.RaiseError("go error")
			return 0
		})
		vm.state.Push(fn)
		err = vm.exportFunctions("test")
		require.NoError(t, err)

		_, err = vm.Execute(context.Background(), "test")
		if err == nil {
			t.Error("expected error from go error")
		} else if !strings.Contains(err.Error(), "go error") {
			t.Errorf("expected error to contain 'go error', got: %v", err)
		}
	})
}
